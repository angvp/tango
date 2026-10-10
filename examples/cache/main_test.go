package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/local"
)

// clock is the cache's and the calculator's time, moved by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// app is the real application with an injected cache and clock.
type app struct {
	handler http.Handler
	calc    *calculator
	clk     *clock
	logs    *bytes.Buffer
}

func newApp(t *testing.T, wrap func(cache.Store) cache.Store, version string) *app {
	t.Helper()
	clk := &clock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	base, err := local.New(local.Config{MaxEntries: 100, MaxBytes: 1 << 16, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	return buildApp(t, base, wrap, clk, version)
}

func buildApp(t *testing.T, base cache.Store, wrap func(cache.Store) cache.Store, clk *clock, version string) *app {
	t.Helper()
	var store cache.Store = cache.Prefix(base, "rates:v"+version+":")
	if wrap != nil {
		store = wrap(store)
	}
	logs := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	calc := &calculator{now: clk.Now}
	registry, err := tango.BuildRegistry(appConfig(store, calc))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &app{handler: handler, calc: calc, clk: clk, logs: logs}
}

func (a *app) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestAppPassesChecks(t *testing.T) {
	store, err := newStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := tango.Check(appConfig(store, &calculator{now: time.Now})); err != nil {
		t.Fatal(err)
	}
}

func TestFirstReadComputesAndSecondIsServedFromTheCache(t *testing.T) {
	a := newApp(t, nil, "1")
	first, second := a.get("/rates/eur/"), a.get("/rates/EUR/")
	if first.Code != 200 || first.Header().Get("X-Cache") != "miss" {
		t.Fatalf("first: %d, X-Cache %q; want a computed miss", first.Code, first.Header().Get("X-Cache"))
	}
	if second.Code != 200 || second.Header().Get("X-Cache") != "hit" {
		t.Fatalf("second: %d, X-Cache %q; want a hit", second.Code, second.Header().Get("X-Cache"))
	}
	if first.Body.String() != second.Body.String() || a.calc.loads.Load() != 1 {
		t.Fatalf("the expensive calculation ran %d times, want once; bodies equal: %v", a.calc.loads.Load(), first.Body.String() == second.Body.String())
	}
}

func TestAValueIsRecomputedOnceItsTTLHasElapsed(t *testing.T) {
	a := newApp(t, nil, "1")
	a.get("/rates/GBP/")
	a.clk.Advance(rateTTL - time.Second)
	if got := a.get("/rates/GBP/").Header().Get("X-Cache"); got != "hit" {
		t.Fatalf("just before the TTL: X-Cache %q, want hit", got)
	}
	a.clk.Advance(time.Second)
	if got := a.get("/rates/GBP/").Header().Get("X-Cache"); got != "miss" {
		t.Fatalf("at the TTL: X-Cache %q, want a recompute", got)
	}
	if a.calc.loads.Load() != 2 {
		t.Fatalf("calculations = %d, want 2", a.calc.loads.Load())
	}
}

func TestAnUnknownCurrencyIs404AndIsNeverCached(t *testing.T) {
	a := newApp(t, nil, "1")
	for i := 0; i < 2; i++ {
		if rec := a.get("/rates/XXX/"); rec.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404", rec.Code)
		}
	}
	if a.calc.loads.Load() != 2 {
		t.Fatalf("calculations = %d; a failed load must not be cached", a.calc.loads.Load())
	}
}

func TestBumpingTheVersionedPrefixMakesOldEntriesUnreachable(t *testing.T) {
	clk := &clock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	base, err := local.New(local.Config{MaxEntries: 100, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	v1 := buildApp(t, base, nil, clk, "1")
	v1.get("/rates/EUR/")
	if got := v1.get("/rates/EUR/").Header().Get("X-Cache"); got != "hit" {
		t.Fatalf("v1 second read: %q, want hit", got)
	}
	// The same underlying cache, a new version: nothing was deleted, yet the
	// old entry cannot be read, and it expires by its TTL.
	v2 := buildApp(t, base, nil, clk, "2")
	if got := v2.get("/rates/EUR/").Header().Get("X-Cache"); got != "miss" {
		t.Fatalf("v2 first read: %q, want miss", got)
	}
}

// brokenStore is a cache that is down: every call fails.
type brokenStore struct{}

var errDown = errors.New("cache is down")

func (brokenStore) Get(context.Context, string) ([]byte, bool, error) { return nil, false, errDown }
func (brokenStore) Set(context.Context, string, []byte, time.Duration) error {
	return errDown
}
func (brokenStore) Delete(context.Context, string) error { return errDown }

func TestACacheOutageDoesNotFailTheRequestAndIsReported(t *testing.T) {
	a := newApp(t, func(cache.Store) cache.Store { return brokenStore{} }, "1")
	rec := a.get("/rates/JPY/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"currency":"JPY"`) {
		t.Fatalf("with the cache down: %d %s; want the computed rate", rec.Code, rec.Body)
	}
	if !strings.Contains(a.logs.String(), "cache failure") || !strings.Contains(a.logs.String(), "op=get") || !strings.Contains(a.logs.String(), "op=set") {
		t.Fatalf("the failure was not reported through OnError:\n%s", a.logs)
	}
	if a.calc.loads.Load() != 1 {
		t.Fatalf("calculations = %d, want 1", a.calc.loads.Load())
	}
}

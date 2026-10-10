// Package cachetest holds the conformance suite every cache.Store must pass.
// Run it against your own Store, and against the real server of a remote
// adapter, to prove the same contract as cache/local.
package cachetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
)

// Factory builds the Store under test. Every call returns a Store whose keys
// start empty (a fresh Store, or one on a unique key space).
type Factory struct {
	New func(t *testing.T) cache.Store
}

// expiryWait is how long the suite waits for a short TTL to take effect. It
// is deliberately generous: correctness never depends on sub-second timing.
const expiryWait = 8 * time.Second

// Run runs the conformance suite against the Store f builds.
func Run(t *testing.T, f Factory) {
	t.Helper()
	t.Run("SetGetDelete", func(t *testing.T) { setGetDelete(t, f.New(t)) })
	t.Run("MissIsNotAnError", func(t *testing.T) { missIsNotAnError(t, f.New(t)) })
	t.Run("EmptyValue", func(t *testing.T) { emptyValue(t, f.New(t)) })
	t.Run("CallerOwnsBothSlices", func(t *testing.T) { ownership(t, f.New(t)) })
	t.Run("InvalidKeys", func(t *testing.T) { invalidKeys(t, f.New(t)) })
	t.Run("TTLRange", func(t *testing.T) { ttlRange(t, f.New(t)) })
	t.Run("ATTLIsNotElapsedEarly", func(t *testing.T) { ttlNotElapsedEarly(t, f.New(t)) })
	t.Run("AShortTTLExpires", func(t *testing.T) { shortTTLExpires(t, f.New(t)) })
	t.Run("ACancelledContext", func(t *testing.T) { cancelled(t, f.New(t)) })
	t.Run("OversizedValue", func(t *testing.T) { oversized(t, f.New(t)) })
	t.Run("ConcurrentUse", func(t *testing.T) { concurrent(t, f.New(t)) })
}

// key returns a key unlikely to collide with another test's on a shared
// server.
func key(name string) string {
	return "cachetest:" + name + ":" + randomSuffix()
}

func randomSuffix() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

func setGetDelete(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("basic")
	if err := s.Set(ctx, k, []byte("one"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Get(ctx, k); err != nil || !ok || string(got) != "one" {
		t.Fatalf("Get = %q, %v, %v; want one", got, ok, err)
	}
	if err := s.Set(ctx, k, []byte("two"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Get(ctx, k); string(got) != "two" {
		t.Fatalf("overwrite: Get = %q, want two", got)
	}
	if err := s.Delete(ctx, k); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get(ctx, k); err != nil || ok {
		t.Fatalf("after Delete: ok=%v err=%v, want a miss", ok, err)
	}
	if err := s.Delete(ctx, k); err != nil {
		t.Fatalf("Delete of a missing key: %v, want nil", err)
	}
}

func missIsNotAnError(t *testing.T, s cache.Store) {
	got, ok, err := s.Get(context.Background(), key("absent"))
	if err != nil || ok || len(got) != 0 {
		t.Fatalf("Get of an absent key = %q, %v, %v; want a miss with no error", got, ok, err)
	}
}

func emptyValue(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("empty")
	if err := s.Set(ctx, k, nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, k)
	if err != nil || !ok || len(got) != 0 {
		t.Fatalf("an empty value: %q, %v, %v; want a hit with no bytes", got, ok, err)
	}
}

func ownership(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("owner")
	in := []byte("original")
	if err := s.Set(ctx, k, in, time.Minute); err != nil {
		t.Fatal(err)
	}
	copy(in, "XXXXXXXX") // Set must not retain the caller's slice
	got, _, _ := s.Get(ctx, k)
	if string(got) != "original" {
		t.Fatalf("mutating the slice given to Set changed the cached value to %q", got)
	}
	copy(got, "YYYYYYYY") // Get returns a slice the caller owns
	again, _, _ := s.Get(ctx, k)
	if string(again) != "original" {
		t.Fatalf("mutating the slice returned by Get changed the cached value to %q", again)
	}
}

func invalidKeys(t *testing.T, s cache.Store) {
	ctx := context.Background()
	for _, k := range []string{"", strings.Repeat("k", cache.MaxKeyLength+1), "has space", "tab\there", "new\nline", "nul\x00", "café", "del\x7f"} {
		if _, _, err := s.Get(ctx, k); !errors.Is(err, cache.ErrInvalidKey) {
			t.Errorf("Get(%q): %v, want ErrInvalidKey", k, err)
		}
		if err := s.Set(ctx, k, []byte("v"), time.Minute); !errors.Is(err, cache.ErrInvalidKey) {
			t.Errorf("Set(%q): %v, want ErrInvalidKey", k, err)
		}
		if err := s.Delete(ctx, k); !errors.Is(err, cache.ErrInvalidKey) {
			t.Errorf("Delete(%q): %v, want ErrInvalidKey", k, err)
		}
	}
	if err := s.Set(ctx, strings.Repeat("k", cache.MaxKeyLength), []byte("v"), time.Minute); err != nil {
		t.Errorf("a 200-byte key: %v, want it accepted", err)
	}
}

func ttlRange(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("ttl")
	for _, ttl := range []time.Duration{0, -time.Second, cache.MaxTTL + time.Nanosecond} {
		if err := s.Set(ctx, k, []byte("v"), ttl); !errors.Is(err, cache.ErrInvalidTTL) {
			t.Errorf("Set with ttl %v: %v, want ErrInvalidTTL", ttl, err)
		}
		if _, ok, _ := s.Get(ctx, k); ok {
			t.Errorf("a refused Set with ttl %v stored a value", ttl)
		}
	}
	if err := s.Set(ctx, k, []byte("v"), cache.MaxTTL); err != nil {
		t.Errorf("Set with exactly 30 days: %v, want it accepted", err)
	}
}

func ttlNotElapsedEarly(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("early")
	if err := s.Set(ctx, k, []byte("v"), time.Hour); err != nil {
		t.Fatal(err)
	}
	// Interpretation of the TTL, not a retention guarantee: a value an hour
	// from its TTL, read straight back, must not look expired.
	if _, ok, err := s.Get(ctx, k); err != nil || !ok {
		t.Fatalf("a value with an hour to live was already gone: ok=%v err=%v", ok, err)
	}
}

func shortTTLExpires(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("short")
	// 1ms is below some backends' granularity (Memcached rounds up to a
	// second); the TTL must be read as at least what was asked, and the value gone soon after.
	if err := s.Set(ctx, k, []byte("v"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(expiryWait)
	for time.Now().Before(deadline) {
		if _, ok, err := s.Get(ctx, k); err != nil {
			t.Fatal(err)
		} else if !ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("a 1ms TTL was still readable after %v", expiryWait)
}

func cancelled(t *testing.T, s cache.Store) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	k := key("cancelled")
	if err := s.Set(ctx, k, []byte("v"), time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("Set with a cancelled context: %v, want context.Canceled", err)
	}
	if _, _, err := s.Get(ctx, k); !errors.Is(err, context.Canceled) {
		t.Errorf("Get with a cancelled context: %v, want context.Canceled", err)
	}
	if err := s.Delete(ctx, k); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete with a cancelled context: %v, want context.Canceled", err)
	}
	if _, ok, _ := s.Get(context.Background(), k); ok {
		t.Error("a Set with a cancelled context stored a value")
	}
}

func oversized(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("big")
	big := bytes.Repeat([]byte("0123456789abcdef"), 2<<16) // 2 MiB
	err := s.Set(ctx, k, big, time.Minute)
	if err != nil {
		if !errors.Is(err, cache.ErrTooLarge) {
			t.Fatalf("an oversized value: %v, want success or ErrTooLarge", err)
		}
		return
	}
	got, ok, err := s.Get(ctx, k)
	if err != nil || !ok || !bytes.Equal(got, big) {
		t.Fatalf("an accepted oversized value came back wrong: ok=%v err=%v, %d of %d bytes", ok, err, len(got), len(big))
	}
}

// raceStep does one Set, Get or Delete of the shared key, by step number, and
// fails on an error or a torn value.
func raceStep(ctx context.Context, s cache.Store, k string, values [][]byte, step int) error {
	switch step % 3 {
	case 0:
		return s.Set(ctx, k, values[step%len(values)], time.Minute)
	case 1:
		got, ok, err := s.Get(ctx, k)
		if err != nil {
			return err
		}
		if ok && !isOneOf(got, values) {
			return fmt.Errorf("Get returned a torn value of %d bytes", len(got))
		}
		return nil
	default:
		return s.Delete(ctx, k)
	}
}

func concurrent(t *testing.T, s cache.Store) {
	ctx, k := context.Background(), key("race")
	values := [][]byte{bytes.Repeat([]byte("a"), 4096), bytes.Repeat([]byte("b"), 4096), bytes.Repeat([]byte("c"), 4096)}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if err := raceStep(ctx, s, k, values, i+j); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func isOneOf(got []byte, values [][]byte) bool {
	for _, v := range values {
		if bytes.Equal(got, v) {
			return true
		}
	}
	return false
}

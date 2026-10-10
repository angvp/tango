package local_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/local"
	"github.com/angvp/tango/cachetest"
)

func TestLocalPassesTheConformanceSuite(t *testing.T) {
	cachetest.Run(t, cachetest.Factory{New: func(t *testing.T) cache.Store {
		s, err := local.New(local.Config{MaxEntries: 1000})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}})
}

// clock is a Now seam a test moves by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)} }
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

func newStore(t *testing.T, cfg local.Config) (*local.Store, *clock) {
	t.Helper()
	c := newClock()
	cfg.Now = c.Now
	s, err := local.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func set(t *testing.T, s *local.Store, key, value string, ttl time.Duration) {
	t.Helper()
	if err := s.Set(context.Background(), key, []byte(value), ttl); err != nil {
		t.Fatalf("Set(%s): %v", key, err)
	}
}

func has(t *testing.T, s *local.Store, key string) bool {
	t.Helper()
	_, ok, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestNewValidatesTheBounds(t *testing.T) {
	for name, cfg := range map[string]local.Config{
		"no entry limit":         {},
		"a negative entry limit": {MaxEntries: -1},
		"a negative byte budget": {MaxEntries: 10, MaxBytes: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := local.New(cfg); err == nil {
				t.Fatal("New accepted an unbounded or invalid configuration")
			}
		})
	}
	if _, err := local.New(local.Config{MaxEntries: 1}); err != nil {
		t.Fatalf("MaxBytes 0 means no byte budget: %v", err)
	}
}

func TestAValueExpiresExactlyWhenItsTTLHasElapsed(t *testing.T) {
	s, clk := newStore(t, local.Config{MaxEntries: 10})
	set(t, s, "k", "v", time.Minute)
	clk.Advance(time.Minute - time.Nanosecond)
	if !has(t, s, "k") {
		t.Fatal("expired before its TTL elapsed")
	}
	clk.Advance(time.Nanosecond)
	if has(t, s, "k") {
		t.Fatal("still present once its TTL elapsed")
	}
}

func TestMaxEntriesEvictsTheLeastRecentlyUsed(t *testing.T) {
	s, _ := newStore(t, local.Config{MaxEntries: 3})
	set(t, s, "a", "1", time.Hour)
	set(t, s, "b", "2", time.Hour)
	set(t, s, "c", "3", time.Hour)
	if !has(t, s, "a") { // a Get makes a the most recently used
		t.Fatal("a missing before any eviction")
	}
	set(t, s, "d", "4", time.Hour) // evicts b, the least recently used
	if has(t, s, "b") {
		t.Fatal("b survived; the least recently used entry should have been evicted")
	}
	for _, k := range []string{"a", "c", "d"} {
		if !has(t, s, k) {
			t.Fatalf("%s was evicted", k)
		}
	}
}

func TestMaxBytesCountsTheKeyAndTheValue(t *testing.T) {
	s, _ := newStore(t, local.Config{MaxEntries: 100, MaxBytes: 10})
	set(t, s, "aa", "123456", time.Hour) // 2 + 6 = 8 bytes
	set(t, s, "bb", "12", time.Hour)     // 2 + 2 = 4: over 10, evicts aa
	if has(t, s, "aa") {
		t.Fatal("the byte budget ignored the key's bytes or did not evict")
	}
	if !has(t, s, "bb") {
		t.Fatal("the newest entry was evicted")
	}
}

func TestReplacingAKeyIsAccountedBeforeEvicting(t *testing.T) {
	s, _ := newStore(t, local.Config{MaxEntries: 100, MaxBytes: 12})
	set(t, s, "a", "1234", time.Hour) // 5
	set(t, s, "b", "1234", time.Hour) // 5 -> 10
	// Replacing b with a value of the same size must not evict a: the old b
	// is being replaced, not added to.
	set(t, s, "b", "5678", time.Hour)
	if !has(t, s, "a") || !has(t, s, "b") {
		t.Fatal("replacing a key evicted something although the total did not grow")
	}
	set(t, s, "b", "12", time.Hour) // shrinking is fine too
	if !has(t, s, "a") {
		t.Fatal("shrinking a replaced key evicted another")
	}
	set(t, s, "b", "123456", time.Hour) // 7 + 5 = 12: still fits
	if !has(t, s, "a") || !has(t, s, "b") {
		t.Fatal("a replacement that exactly fits the budget evicted something")
	}
}

func TestAnEntryLargerThanTheBudgetIsRefusedAndTheCacheIsUntouched(t *testing.T) {
	s, _ := newStore(t, local.Config{MaxEntries: 100, MaxBytes: 10})
	set(t, s, "a", "1234", time.Hour)
	set(t, s, "keep", "x", time.Hour)
	err := s.Set(context.Background(), "a", []byte(strings.Repeat("z", 11)), time.Hour)
	if !errors.Is(err, cache.ErrTooLarge) {
		t.Fatalf("an oversized replacement: %v, want ErrTooLarge", err)
	}
	if got, ok, _ := s.Get(context.Background(), "a"); !ok || string(got) != "1234" {
		t.Fatalf("a refused Set changed the existing value: %q, %v", got, ok)
	}
	if !has(t, s, "keep") {
		t.Fatal("a refused Set evicted another entry")
	}
	if err := s.Set(context.Background(), "new", []byte(strings.Repeat("z", 11)), time.Hour); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatalf("an oversized new entry: %v, want ErrTooLarge", err)
	}
	if has(t, s, "new") || !has(t, s, "keep") {
		t.Fatal("a refused new entry changed the cache")
	}
}

func TestExpiredEntriesAreRemovedBeforeLiveOnesWhenMakingRoom(t *testing.T) {
	s, clk := newStore(t, local.Config{MaxEntries: 3})
	set(t, s, "old", "1", time.Minute) // least recently used, but it will expire
	set(t, s, "live", "2", time.Hour)
	set(t, s, "alsoLive", "3", time.Hour)
	if !has(t, s, "old") {
		t.Fatal("old missing")
	}
	// Make "old" the most recently used, so plain LRU would evict "live".
	clk.Advance(2 * time.Minute) // old is now expired
	set(t, s, "fresh", "4", time.Hour)
	if !has(t, s, "live") || !has(t, s, "alsoLive") || !has(t, s, "fresh") {
		t.Fatal("a live entry was evicted although an expired one could have been removed")
	}
	if has(t, s, "old") {
		t.Fatal("the expired entry survived")
	}
}

func TestStorageCopiesOnSetAndGet(t *testing.T) {
	s, _ := newStore(t, local.Config{MaxEntries: 10})
	in := []byte("original")
	if err := s.Set(context.Background(), "k", in, time.Hour); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	got, _, _ := s.Get(context.Background(), "k")
	got[1] = 'Y'
	again, _, _ := s.Get(context.Background(), "k")
	if string(again) != "original" {
		t.Fatalf("value aliased the caller's memory: %q", again)
	}
}

func TestConcurrentUseUnderTheRaceDetector(t *testing.T) {
	s, _ := newStore(t, local.Config{MaxEntries: 8, MaxBytes: 256})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				k := string(rune('a' + (i+j)%12))
				_ = s.Set(context.Background(), k, []byte(strings.Repeat("v", (i+j)%40)), time.Hour)
				_, _, _ = s.Get(context.Background(), k)
				if j%7 == 0 {
					_ = s.Delete(context.Background(), k)
				}
			}
		}()
	}
	wg.Wait()
}

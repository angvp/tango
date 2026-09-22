package ratelimit

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

func TestNewLimiterValidation(t *testing.T) {
	cases := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "valid", opts: Options{Limit: 5, Refill: time.Second}, wantErr: false},
		{name: "zero limit", opts: Options{Limit: 0, Refill: time.Second}, wantErr: true},
		{name: "negative limit", opts: Options{Limit: -1, Refill: time.Second}, wantErr: true},
		{name: "zero refill", opts: Options{Limit: 5, Refill: 0}, wantErr: true},
		{name: "negative refill", opts: Options{Limit: 5, Refill: -time.Second}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewLimiter(tc.opts)
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTakeCostValidation(t *testing.T) {
	cases := []struct {
		name    string
		cost    int
		wantErr bool
	}{
		{name: "cost <= 0 (zero)", cost: 0, wantErr: true},
		{name: "cost <= 0 (negative)", cost: -1, wantErr: true},
		{name: "cost == Limit", cost: 5, wantErr: false},
		{name: "cost > Limit", cost: 6, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := NewLimiter(Options{Limit: 5, Refill: time.Second})
			if err != nil {
				t.Fatalf("NewLimiter: %v", err)
			}
			_, err = l.Take(context.Background(), "k", time.Unix(0, 0), tc.cost)
			if tc.wantErr && err == nil {
				t.Fatalf("cost=%d: expected an error", tc.cost)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("cost=%d: unexpected error: %v", tc.cost, err)
			}
		})
	}
}

// TestTakeCostExceedingLimitNeverModifiesBucketState proves a cost > Limit
// error is rejected before touching bucket state at all — a later,
// legitimate request for the same key must see the bucket exactly as if
// the over-limit call had never happened.
func TestTakeCostExceedingLimitNeverModifiesBucketState(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 5, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	now := time.Unix(0, 0)

	if _, err := l.Take(ctx, "k", now, 100); err == nil {
		t.Fatal("expected an error for cost > Limit")
	}

	// The bucket must still be untouched: a fresh full-capacity request
	// succeeds exactly as if "k" had never been seen.
	d, err := l.Take(ctx, "k", now, 5)
	if err != nil {
		t.Fatalf("take after rejected over-limit cost: %v", err)
	}
	if !d.Allowed || d.Remaining != 0 {
		t.Fatalf("expected a full, untouched bucket (Allowed with 0 remaining after consuming all 5), got %+v", d)
	}
}

func TestTakeBurstUpToLimitThenRejects(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 3, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	now := time.Unix(0, 0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		d, err := l.Take(ctx, "k", now, 1)
		if err != nil {
			t.Fatalf("take %d: %v", i, err)
		}
		if !d.Allowed {
			t.Fatalf("take %d: expected allowed, got %+v", i, d)
		}
	}

	d, err := l.Take(ctx, "k", now, 1)
	if err != nil {
		t.Fatalf("take 4: %v", err)
	}
	if d.Allowed {
		t.Fatalf("expected the 4th take at the same instant to be rejected, got %+v", d)
	}
	if d.RetryAfter <= 0 {
		t.Fatalf("expected a positive RetryAfter, got %v", d.RetryAfter)
	}
}

func TestTakeRefillsAfterElapsedTime(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 1, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	start := time.Unix(0, 0)

	if d, err := l.Take(ctx, "k", start, 1); err != nil || !d.Allowed {
		t.Fatalf("first take: d=%+v err=%v", d, err)
	}
	if d, err := l.Take(ctx, "k", start, 1); err != nil || d.Allowed {
		t.Fatalf("immediate second take should be rejected: d=%+v err=%v", d, err)
	}

	// Exactly one refill period later, a token is available again.
	later := start.Add(time.Second)
	d, err := l.Take(ctx, "k", later, 1)
	if err != nil {
		t.Fatalf("take after refill: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed after a full refill period, got %+v", d)
	}
}

func TestTakeRetryAfterRounding(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 1, Refill: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	start := time.Unix(0, 0)

	if d, err := l.Take(ctx, "k", start, 1); err != nil || !d.Allowed {
		t.Fatalf("first take: d=%+v err=%v", d, err)
	}

	// Half a refill period later: half a token accumulated, still short of
	// the 1 needed, so RetryAfter should reflect the remaining half period.
	halfway := start.Add(time.Second)
	d, err := l.Take(ctx, "k", halfway, 1)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if d.Allowed {
		t.Fatalf("expected rejection at the halfway point, got %+v", d)
	}
	if d.RetryAfter != time.Second {
		t.Fatalf("RetryAfter = %v, want exactly 1s (half of the 2s refill period remaining)", d.RetryAfter)
	}
}

func TestTakeCostConsumesMultipleTokens(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 5, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	now := time.Unix(0, 0)

	d, err := l.Take(ctx, "k", now, 3)
	if err != nil {
		t.Fatalf("take cost=3: %v", err)
	}
	if !d.Allowed || d.Remaining != 2 {
		t.Fatalf("expected allowed with 2 remaining, got %+v", d)
	}

	// Only 2 tokens left; a cost=3 request must be rejected.
	d, err = l.Take(ctx, "k", now, 3)
	if err != nil {
		t.Fatalf("take cost=3 again: %v", err)
	}
	if d.Allowed {
		t.Fatalf("expected rejection with only 2 tokens left for cost=3, got %+v", d)
	}
}

func TestTakeIndependentKeys(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 1, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	now := time.Unix(0, 0)

	if d, err := l.Take(ctx, "a", now, 1); err != nil || !d.Allowed {
		t.Fatalf("key a: d=%+v err=%v", d, err)
	}
	if d, err := l.Take(ctx, "b", now, 1); err != nil || !d.Allowed {
		t.Fatalf("key b should be independent of key a: d=%+v err=%v", d, err)
	}
}

func TestTakeConcurrentSameAndDistinctKeys(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 1000, Refill: time.Millisecond})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "shared"
			if i%2 == 0 {
				key = "distinct"
			}
			for j := 0; j < 20; j++ {
				if _, err := l.Take(ctx, key, now, 1); err != nil {
					t.Errorf("take: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestTakeRejectsCanceledContext(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 5, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := l.Take(ctx, "k", time.Now(), 1); err == nil {
		t.Fatal("expected Take to reject an already-canceled context")
	}
}

// bucketCount is a small test helper reaching into Limiter's unexported
// state — package-private, same-package inspection, never exported
// diagnostics that would exist solely for tests.
func bucketCount(l *Limiter) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func hasBucket(l *Limiter, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.buckets[key]
	return ok
}

func TestCleanupRemovesStaleBuckets(t *testing.T) {
	// staleAfter = Limit * Refill = 2 * 1s = 2s.
	l, err := NewLimiter(Options{Limit: 2, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	start := time.Unix(0, 0)

	if _, err := l.Take(ctx, "stale", start, 1); err != nil {
		t.Fatalf("take: %v", err)
	}
	if !hasBucket(l, "stale") {
		t.Fatal("bucket should exist right after Take")
	}

	// Advance well past staleAfter and touch a different key — this is the
	// call that observes lastCleanup is overdue and sweeps.
	later := start.Add(10 * time.Second)
	if _, err := l.Take(ctx, "trigger", later, 1); err != nil {
		t.Fatalf("take: %v", err)
	}

	if hasBucket(l, "stale") {
		t.Fatal("expected the untouched, long-stale bucket to have been evicted")
	}
	if !hasBucket(l, "trigger") {
		t.Fatal("the key that triggered cleanup must not evict itself")
	}
}

func TestCleanupPreservesActiveBuckets(t *testing.T) {
	// staleAfter = 2 * 1s = 2s.
	l, err := NewLimiter(Options{Limit: 2, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	now := time.Unix(0, 0)

	if _, err := l.Take(ctx, "active", now, 1); err != nil {
		t.Fatalf("take: %v", err)
	}

	// Keep "active" alive by touching it every 500ms (well under the 2s
	// staleness threshold) for a total span far longer than staleAfter,
	// interleaved with a "trigger" key at the same cadence so cleanup
	// sweeps run repeatedly across that span.
	for i := 1; i <= 20; i++ {
		now = now.Add(500 * time.Millisecond)
		if _, err := l.Take(ctx, "active", now, 1); err != nil {
			t.Fatalf("take active at step %d: %v", i, err)
		}
		if _, err := l.Take(ctx, "trigger", now, 1); err != nil {
			t.Fatalf("take trigger at step %d: %v", i, err)
		}
	}

	if !hasBucket(l, "active") {
		t.Fatal("a bucket touched well within staleAfter on every cycle must never be evicted")
	}
}

// TestCleanupPreservesActiveBucketRefillAndRejectionBehavior proves the
// first half of "cleanup changes no observed behavior": a bucket touched
// often enough that it's never actually stale must see exactly the same
// Allowed/Remaining/RetryAfter sequence a plain token bucket would produce,
// regardless of how many cleanup sweeps run in the background (triggered
// here by unrelated "trigger" keys). All timestamps are monotonically
// increasing — never moved backward — and every expected Decision is
// computed by hand from ordinary token-bucket math, not inferred from
// whatever the code happens to return.
func TestCleanupPreservesActiveBucketRefillAndRejectionBehavior(t *testing.T) {
	// Limit=3, Refill=1s => staleAfter = 3s. "k" is touched every 500ms —
	// always well inside that 3s window — so it must never be evicted.
	l, err := NewLimiter(Options{Limit: 3, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	start := time.Unix(0, 0)

	// Hand-computed: starting from a full 3-token bucket, consuming 1 token
	// every 500ms while refilling 0.5 tokens/step settles into a
	// 5-allow/1-reject repeating pattern (tokens: 2,1.5,1,0.5,0,reject@0.5,
	// then 0,reject@0.5,0,reject@0.5 — see the two five-step cycles below).
	wantAllowed := []bool{true, true, true, true, true, false, true, false, true, false}
	wantRemaining := []int{2, 1, 1, 0, 0, 0, 0, 0, 0, 0}
	wantRetryAfter := []time.Duration{0, 0, 0, 0, 0, 500 * time.Millisecond, 0, 500 * time.Millisecond, 0, 500 * time.Millisecond}

	now := start
	triggerN := 0
	for i := range wantAllowed {
		if i > 0 {
			now = now.Add(500 * time.Millisecond)
		}

		// Fire an unrelated Take just before "k"'s own — over the course of
		// this loop these cross the 3s sweep interval (lastCleanup starts
		// at t=0, the very first Take of the test), so at least one real
		// cleanup sweep runs partway through. It must not touch "k".
		triggerN++
		if _, err := l.Take(ctx, fmt.Sprintf("trigger-%d", triggerN), now, 1); err != nil {
			t.Fatalf("step %d: trigger take: %v", i, err)
		}

		d, err := l.Take(ctx, "k", now, 1)
		if err != nil {
			t.Fatalf("step %d: k take: %v", i, err)
		}
		if d.Allowed != wantAllowed[i] || d.Remaining != wantRemaining[i] || d.RetryAfter != wantRetryAfter[i] {
			t.Fatalf("step %d (now=%v): got Decision{Allowed:%v Remaining:%d RetryAfter:%v}, want {Allowed:%v Remaining:%d RetryAfter:%v}",
				i, now.Sub(start), d.Allowed, d.Remaining, d.RetryAfter, wantAllowed[i], wantRemaining[i], wantRetryAfter[i])
		}
	}

	if !hasBucket(l, "k") {
		t.Fatal("an actively-used bucket must still exist at the end of the test")
	}
}

// TestCleanupEvictedBucketBehavesLikeNaturalFullRefill proves the second
// half: a bucket that a cleanup sweep actually did evict (confirmed via
// direct, package-private inspection, not inferred) must behave, once
// reused, exactly as a bucket that was simply left in place and allowed to
// refill-and-cap at Limit the ordinary way. Timestamps only increase.
func TestCleanupEvictedBucketBehavesLikeNaturalFullRefill(t *testing.T) {
	// Limit=3, Refill=1s => staleAfter = 3s.
	l, err := NewLimiter(Options{Limit: 3, Refill: time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	start := time.Unix(0, 0)

	if _, err := l.Take(ctx, "k", start, 3); err != nil {
		t.Fatalf("exhaust: %v", err)
	}
	if !hasBucket(l, "k") {
		t.Fatal("bucket should exist right after Take")
	}

	// A trigger call exactly staleAfter later makes lastCleanup overdue and
	// sweeps: "k" has been idle exactly staleAfter, so it's evicted.
	sweepAt := start.Add(3 * time.Second)
	if _, err := l.Take(ctx, "trigger", sweepAt, 1); err != nil {
		t.Fatalf("trigger take: %v", err)
	}
	if hasBucket(l, "k") {
		t.Fatal("expected the idle-for-exactly-staleAfter bucket to have been evicted by the sweep")
	}

	// Reused later still (monotonically after the eviction point). A
	// bucket left in place the whole time would have refilled
	// min(Limit, 0 + elapsed/Refill) = capped at Limit=3 tokens by now
	// regardless — the same value eviction-then-recreate produces.
	reuseAt := start.Add(5 * time.Second)
	d, err := l.Take(ctx, "k", reuseAt, 1)
	if err != nil {
		t.Fatalf("reuse take: %v", err)
	}
	if !d.Allowed || d.Remaining != 2 || d.RetryAfter != 0 {
		t.Fatalf("reused evicted bucket = %+v, want {Allowed:true Remaining:2 RetryAfter:0} (identical to a naturally fully-refilled bucket)", d)
	}
}

func TestCleanupConcurrentCallsRemainRaceSafe(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 4, Refill: time.Millisecond})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			base := time.Unix(0, 0).Add(time.Duration(i) * time.Millisecond)
			for j := 0; j < 50; j++ {
				key := fmt.Sprintf("k%d", (i+j)%10)
				now := base.Add(time.Duration(j) * time.Millisecond)
				if _, err := l.Take(ctx, key, now, 1); err != nil {
					// cost/ctx errors aren't expected here; a real error
					// is a test failure, surfaced via -race for any data
					// race regardless.
					t.Errorf("take: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestStaleAfterNormalConfiguration(t *testing.T) {
	l, err := NewLimiter(Options{Limit: 5, Refill: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	if got, want := l.staleAfter(), 10*time.Second; got != want {
		t.Fatalf("staleAfter() = %v, want %v", got, want)
	}
}

func TestStaleAfterSaturatesInsteadOfOverflowing(t *testing.T) {
	// int64(Limit) * int64(Refill) here is roughly 2.1e9 * 3.6e12 ≈
	// 7.7e21ns, far past time.Duration's int64-nanosecond ceiling
	// (~9.22e18ns, ~292 years) — a real overflow, not a contrived one.
	l, err := NewLimiter(Options{Limit: math.MaxInt32, Refill: time.Hour})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	got := l.staleAfter()
	if got <= 0 {
		t.Fatalf("staleAfter() = %v, want a positive saturated value, not an overflowed zero/negative one", got)
	}
	if got != math.MaxInt64 {
		t.Fatalf("staleAfter() = %v, want the saturated ceiling %v", got, time.Duration(math.MaxInt64))
	}
}

func TestSaturatingMul(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want time.Duration
	}{
		{name: "zero a", a: 0, b: 5, want: 0},
		{name: "zero b", a: 5, b: 0, want: 0},
		{name: "no overflow", a: 5, b: 2, want: 10},
		{name: "exact boundary", a: math.MaxInt64, b: 1, want: math.MaxInt64},
		{name: "overflow", a: math.MaxInt64, b: 2, want: math.MaxInt64},
		{name: "large overflow", a: int64(math.MaxInt32), b: int64(time.Hour), want: math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := saturatingMul(tc.a, tc.b); got != tc.want {
				t.Fatalf("saturatingMul(%d, %d) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestOverflowingConfigurationCleanupNeverEvictsOrResetsActiveKeys proves
// cleanup stays purely a memory optimization even when staleAfter has
// saturated: repeated requests against an overflowing Limit/Refill
// configuration must behave exactly like an ordinary token bucket — burst
// up to Limit, then rejected — never evicted or reset by cleanup, since a
// saturated (effectively ~292-year) staleness threshold can never be
// crossed by any realistic test timespan.
func TestOverflowingConfigurationCleanupNeverEvictsOrResetsActiveKeys(t *testing.T) {
	l, err := NewLimiter(Options{Limit: math.MaxInt32, Refill: time.Hour})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	ctx := context.Background()
	now := time.Unix(0, 0)

	// Burst: the first Limit requests must all be allowed, draining the
	// bucket exactly one token at a time — no premature eviction/reset
	// would show up as a Decision suddenly reporting a full bucket again.
	for i := 0; i < 5; i++ {
		now = now.Add(time.Millisecond)
		d, err := l.Take(ctx, "k", now, 1)
		if err != nil {
			t.Fatalf("take %d: %v", i, err)
		}
		if !d.Allowed {
			t.Fatalf("take %d: expected allowed (well within the huge Limit), got %+v", i, d)
		}
		wantRemaining := math.MaxInt32 - 1 - i
		if d.Remaining != wantRemaining {
			t.Fatalf("take %d: Remaining = %d, want %d — a reset/eviction would show up as a jump back toward full", i, d.Remaining, wantRemaining)
		}
	}

	if !hasBucket(l, "k") {
		t.Fatal("an actively-used bucket under an overflowing configuration must still exist")
	}
}

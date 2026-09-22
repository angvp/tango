package ratelimit

import (
	"context"
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

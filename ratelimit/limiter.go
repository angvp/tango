// Package ratelimit is a reusable, host-composable request rate limiter,
// independent of auth, admin, accounts, and realtime. It never imports any
// of those packages: a host supplies its own key extraction (see KeyFunc),
// so ratelimit stays usable for plain IP-based limiting, identity-aware
// limiting on top of a host's own auth, or anything else reachable from an
// *http.Request.
//
// This is deliberately not a replacement for admin/accounts' own
// failed-login-attempt limiter (internal/security.RateLimiter): that one
// counts authentication failures in a sliding window, this one counts
// every request via a token bucket. The semantics differ on purpose and
// are not merged.
package ratelimit

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// Decision is the outcome of one Limiter.Take call — enough information
// for middleware, or any custom response, to build rate-limit headers or a
// body without reaching into Limiter's internal state (which stays
// unexported).
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// Options configures a Limiter's token-bucket shape and clock.
type Options struct {
	// Limit is the bucket capacity — the maximum burst of tokens a key can
	// accumulate and spend at once. Must be positive.
	Limit int
	// Refill is the time it takes to accumulate one additional token.
	// Must be positive.
	Refill time.Duration
	// Clock returns the current time; it defaults to time.Now. Tests
	// inject a fake clock here so token-bucket refill/burst/exhaustion
	// behavior never depends on a real-time sleep. It is used internally
	// by Middleware (see middleware.go) — Limiter.Take itself always takes
	// an explicit now, for callers (and tests) that want full manual
	// control without going through Options at all.
	Clock func() time.Time
}

// Limiter is a concrete, in-memory, per-process token-bucket rate limiter.
// There is no storage interface in this package — see this milestone's ADR
// on why: a correct distributed token bucket needs one atomic
// check-and-consume operation, not a generic Get/Set store, and that seam
// is deferred until a real second backend exists to prove its shape.
type Limiter struct {
	limit  int
	refill time.Duration
	clock  func() time.Time

	mu          sync.Mutex
	buckets     map[string]bucketState
	lastCleanup time.Time
}

type bucketState struct {
	tokens float64
	last   time.Time
}

// NewLimiter constructs a Limiter. Limit and Refill must be positive;
// Clock defaults to time.Now when nil.
func NewLimiter(opts Options) (*Limiter, error) {
	if opts.Limit <= 0 {
		return nil, fmt.Errorf("tango ratelimit: limit must be positive")
	}
	if opts.Refill <= 0 {
		return nil, fmt.Errorf("tango ratelimit: refill must be positive")
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Limiter{
		limit:   opts.Limit,
		refill:  opts.Refill,
		clock:   clock,
		buckets: make(map[string]bucketState),
	}, nil
}

// Take consumes cost tokens for key at time now, returning the resulting
// Decision. cost must be positive — a caller charging more than 1 token
// per request (e.g. for an expensive operation) passes a larger cost, but
// cost <= 0 is a caller error, never silently treated as 1. cost must also
// not exceed Limit: a bucket's capacity never holds more than Limit tokens,
// so a request costing more than that could never succeed no matter how
// long it waited — that's a caller/configuration error, not an ordinary
// rejection with a finite RetryAfter.
//
// A key seen for the first time starts with a full bucket (Limit tokens),
// so the very first requests for any key may burst up to Limit before
// being throttled.
func (l *Limiter) Take(ctx context.Context, key string, now time.Time, cost int) (Decision, error) {
	if cost <= 0 {
		return Decision{}, fmt.Errorf("tango ratelimit: cost must be positive, got %d", cost)
	}
	if cost > l.limit {
		return Decision{}, fmt.Errorf("tango ratelimit: cost %d exceeds limit %d — this request could never succeed", cost, l.limit)
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.buckets[key]
	if !ok {
		state = bucketState{tokens: float64(l.limit), last: now}
	} else if elapsed := now.Sub(state.last); elapsed > 0 {
		refilled := float64(elapsed) / float64(l.refill)
		state.tokens += refilled
		if state.tokens > float64(l.limit) {
			state.tokens = float64(l.limit)
		}
		state.last = now
	}

	decision := Decision{Limit: l.limit}
	if state.tokens >= float64(cost) {
		state.tokens -= float64(cost)
		decision.Allowed = true
	} else {
		missing := float64(cost) - state.tokens
		decision.RetryAfter = time.Duration(missing * float64(l.refill))
	}
	decision.Remaining = int(state.tokens)

	l.buckets[key] = state
	l.cleanupLocked(now)

	return decision, nil
}

// staleAfter is how long a key's bucket can sit untouched before it's
// guaranteed to have refilled back to full capacity — Limit tokens at one
// per Refill each. Past that point, evicting the bucket and letting the
// next Take for that key recreate it fresh (also full, per Take's own
// first-seen-key behavior) is behaviorally identical to leaving the stale
// entry in place — the only effect of eviction is freeing the memory.
//
// Limit * Refill can overflow time.Duration's int64 nanosecond range for
// large-but-valid combinations (e.g. a very high Limit paired with a
// multi-hour Refill) — this is purely an internal memory-cleanup detail, so
// such a Limiter must still work exactly like any other, just with its
// buckets saturating to the largest representable positive Duration before
// they're ever considered stale enough to evict, rather than NewLimiter
// rejecting an otherwise valid configuration or the multiplication silently
// wrapping into a small/negative value that would evict, and therefore
// alter the behavior of, buckets that are still genuinely in active use.
func (l *Limiter) staleAfter() time.Duration {
	return saturatingMul(int64(l.limit), int64(l.refill))
}

// saturatingMul returns a*b clamped to math.MaxInt64 instead of overflowing
// when both a and b are positive (the only case staleAfter ever calls it
// with — Limit and Refill are both validated positive by NewLimiter).
func saturatingMul(a, b int64) time.Duration {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(a * b)
}

// cleanupLocked opportunistically sweeps buckets untouched for at least
// staleAfter, at most once per staleAfter window — called from within Take,
// under l.mu, so it adds no background goroutine, lifecycle hook, or
// separate scheduling mechanism. It uses the caller-supplied now throughout,
// so it never depends on real wall-clock time and needs no sleeping in
// tests. An always-in-use Limiter (few distinct keys, called often) pays an
// O(len(buckets)) sweep only once every staleAfter — not on every Take.
func (l *Limiter) cleanupLocked(now time.Time) {
	threshold := l.staleAfter()
	if l.lastCleanup.IsZero() {
		l.lastCleanup = now
		return
	}
	if now.Sub(l.lastCleanup) < threshold {
		return
	}
	l.lastCleanup = now
	for key, state := range l.buckets {
		if now.Sub(state.last) >= threshold {
			delete(l.buckets, key)
		}
	}
}

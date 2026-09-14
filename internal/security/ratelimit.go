package security

import (
	"sync"
	"time"
)

// RateLimiter tracks recent failed attempts per caller-provided key,
// usually a source IP address. It is intentionally in-memory and
// single-process: callers that need cross-process throttling should provide
// their own middleware or service-level protection.
type RateLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	attempts    map[string][]time.Time
}

// NewRateLimiter constructs a per-key sliding-window limiter. A key is
// allowed while it has fewer than maxAttempts recorded failures within
// window.
func NewRateLimiter(maxAttempts int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		attempts:    make(map[string][]time.Time),
	}
}

// Allow reports whether key is currently under the failed-attempt threshold,
// pruning older attempts as it goes.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-l.window)
	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	l.attempts[key] = kept

	return len(kept) < l.maxAttempts
}

// RecordFailure records a failed attempt for key.
func (l *RateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], time.Now())
}

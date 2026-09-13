package admin

import (
	"testing"
	"time"
)

func TestLoginRateLimiterAllowsUnderThreshold(t *testing.T) {
	limiter := newLoginRateLimiter()

	for i := 0; i < loginRateLimitAttempts; i++ {
		if !limiter.Allow("1.2.3.4") {
			t.Fatalf("attempt %d disallowed, want allowed (under threshold)", i)
		}
		limiter.RecordFailure("1.2.3.4")
	}
}

func TestLoginRateLimiterBlocksAtThreshold(t *testing.T) {
	limiter := newLoginRateLimiter()

	for i := 0; i < loginRateLimitAttempts; i++ {
		limiter.Allow("1.2.3.4")
		limiter.RecordFailure("1.2.3.4")
	}

	if limiter.Allow("1.2.3.4") {
		t.Fatal("Allow = true at threshold, want false")
	}
}

func TestLoginRateLimiterTracksKeysIndependently(t *testing.T) {
	limiter := newLoginRateLimiter()

	for i := 0; i < loginRateLimitAttempts; i++ {
		limiter.Allow("1.2.3.4")
		limiter.RecordFailure("1.2.3.4")
	}

	if !limiter.Allow("5.6.7.8") {
		t.Fatal("Allow = false for an unrelated key, want true")
	}
}

func TestLoginRateLimiterResetsAfterWindowElapses(t *testing.T) {
	limiter := newLoginRateLimiter()

	// Seed attempts as already outside the window, simulating time having
	// passed, without needing to sleep in the test.
	stale := time.Now().Add(-loginRateLimitWindow - time.Second)
	for i := 0; i < loginRateLimitAttempts; i++ {
		limiter.attempts["1.2.3.4"] = append(limiter.attempts["1.2.3.4"], stale)
	}

	if !limiter.Allow("1.2.3.4") {
		t.Fatal("Allow = false after the window elapsed, want true")
	}
}

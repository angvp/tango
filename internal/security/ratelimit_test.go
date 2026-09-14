package security

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUnderThreshold(t *testing.T) {
	limiter := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		if !limiter.Allow("1.2.3.4") {
			t.Fatalf("attempt %d disallowed, want allowed (under threshold)", i)
		}
		limiter.RecordFailure("1.2.3.4")
	}
}

func TestRateLimiterBlocksAtThreshold(t *testing.T) {
	limiter := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		limiter.Allow("1.2.3.4")
		limiter.RecordFailure("1.2.3.4")
	}

	if limiter.Allow("1.2.3.4") {
		t.Fatal("Allow = true at threshold, want false")
	}
}

func TestRateLimiterTracksKeysIndependently(t *testing.T) {
	limiter := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		limiter.Allow("1.2.3.4")
		limiter.RecordFailure("1.2.3.4")
	}

	if !limiter.Allow("5.6.7.8") {
		t.Fatal("Allow = false for an unrelated key, want true")
	}
}

func TestRateLimiterResetsAfterWindowElapses(t *testing.T) {
	limiter := NewRateLimiter(5, time.Minute)

	stale := time.Now().Add(-time.Minute - time.Second)
	for i := 0; i < 5; i++ {
		limiter.attempts["1.2.3.4"] = append(limiter.attempts["1.2.3.4"], stale)
	}

	if !limiter.Allow("1.2.3.4") {
		t.Fatal("Allow = false after the window elapsed, want true")
	}
}

func TestRateLimiterUsesInstanceConfiguration(t *testing.T) {
	strict := NewRateLimiter(1, time.Minute)
	loose := NewRateLimiter(2, time.Minute)

	strict.RecordFailure("1.2.3.4")
	loose.RecordFailure("1.2.3.4")

	if strict.Allow("1.2.3.4") {
		t.Fatal("strict Allow = true after one failure, want false")
	}
	if !loose.Allow("1.2.3.4") {
		t.Fatal("loose Allow = false after one failure, want true")
	}
}

func TestRateLimiterInstancesDoNotShareState(t *testing.T) {
	first := NewRateLimiter(1, time.Minute)
	second := NewRateLimiter(1, time.Minute)

	first.RecordFailure("1.2.3.4")

	if first.Allow("1.2.3.4") {
		t.Fatal("first Allow = true after one failure, want false")
	}
	if !second.Allow("1.2.3.4") {
		t.Fatal("second Allow = false for same key, want independent state")
	}
}

package ratelimit

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func alwaysKey(k string) KeyFunc {
	return func(*http.Request) (string, error) { return k, nil }
}

func newFixedClockLimiter(t *testing.T, limit int, refill time.Duration, now time.Time) *Limiter {
	t.Helper()
	l, err := NewLimiter(Options{Limit: limit, Refill: refill, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return l
}

func TestMiddlewareAllowsAndCallsNext(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(l, alwaysKey("k"))(next)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("next was never called for an allowed request")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMiddlewareRejectsWithDefaultResponse(t *testing.T) {
	l := newFixedClockLimiter(t, 1, 2*time.Second, time.Unix(0, 0))
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalls++
	})
	handler := Middleware(l, alwaysKey("k"))(next)

	// First request consumes the only token.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	// Second request, same instant, must be rejected.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if nextCalls != 1 {
		t.Fatalf("next was called %d times, want exactly 1 (the allowed first request)", nextCalls)
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want %q (full refill period, rounded up)", got, "2")
	}
	if got := rec.Body.String(); got != "{\"error\":\"rate limit exceeded\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestMiddlewareCustomLimitedHandlerReceivesDecision(t *testing.T) {
	l := newFixedClockLimiter(t, 1, time.Second, time.Unix(0, 0))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	var gotDecision Decision
	handler := Middleware(l, alwaysKey("k"), WithLimitedHandler(func(w http.ResponseWriter, r *http.Request, d Decision) {
		gotDecision = d
		w.WriteHeader(http.StatusServiceUnavailable)
	}))(next)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want the custom handler's 503", rec.Code)
	}
	if gotDecision.Allowed {
		t.Fatalf("custom handler received Allowed=true, want the real rejected Decision: %+v", gotDecision)
	}
	if gotDecision.Limit != 1 {
		t.Fatalf("custom handler's Decision.Limit = %d, want 1", gotDecision.Limit)
	}
}

func TestMiddlewareKeyFuncErrorGoesToErrorHandlerNotLimitedHandler(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	wantErr := errors.New("boom")
	failingKey := func(*http.Request) (string, error) { return "", wantErr }

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called on a KeyFunc error")
	})

	limitedCalled := false
	var gotErr error
	handler := Middleware(l, failingKey,
		WithLimitedHandler(func(http.ResponseWriter, *http.Request, Decision) { limitedCalled = true }),
		WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			gotErr = err
			w.WriteHeader(http.StatusBadRequest)
		}),
	)(next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if limitedCalled {
		t.Fatal("limitedHandler must never be called for a KeyFunc error")
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("errorHandler err = %v, want %v", gotErr, wantErr)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMiddlewareEmptyKeyNoErrorIsTreatedAsError(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when the key extractor returns an empty key")
	})

	limitedCalled := false
	errorCalled := false
	handler := Middleware(l, alwaysKey(""),
		WithLimitedHandler(func(http.ResponseWriter, *http.Request, Decision) { limitedCalled = true }),
		WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			errorCalled = true
			w.WriteHeader(http.StatusBadRequest)
		}),
	)(next)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if limitedCalled {
		t.Fatal("an empty key must never be routed to limitedHandler")
	}
	if !errorCalled {
		t.Fatal("an empty key must be routed to errorHandler")
	}
}

func TestMiddlewareDefaultErrorResponse(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	failingKey := func(*http.Request) (string, error) { return "", fmt.Errorf("extractor failed") }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	handler := Middleware(l, failingKey)(next)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"error\":\"rate limit key extraction failed\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestMiddlewareWithCostChangesConsumption(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	var lastDecision Decision
	handler := Middleware(l, alwaysKey("k"), WithCost(3), WithLimitedHandler(func(w http.ResponseWriter, r *http.Request, d Decision) {
		lastDecision = d
		w.WriteHeader(http.StatusTooManyRequests)
	}))(next)

	// 5 tokens, cost 3: first request allowed, leaves 2. Second request
	// (cost 3 again) must be rejected since only 2 remain.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (cost=3 should exhaust a 5-token bucket after two requests)", rec.Code)
	}
	if lastDecision.Allowed {
		t.Fatalf("expected a rejected decision, got %+v", lastDecision)
	}
}

func TestMiddlewareTakeErrorGoesToErrorHandler(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when Take itself errors")
	})

	var gotErr error
	// WithCost(0) makes every Take call fail (cost must be positive),
	// exercising Middleware's own Take-error branch (distinct from a
	// KeyFunc error, which the other tests already cover).
	handler := Middleware(l, alwaysKey("k"), WithCost(0), WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
		gotErr = err
		w.WriteHeader(http.StatusBadRequest)
	}))(next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if gotErr == nil {
		t.Fatal("expected Take's cost error to reach errorHandler")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestMiddlewareCostExceedingLimitGoesToErrorHandler covers finding 1's
// middleware-routing requirement: a WithCost configured higher than the
// Limiter's own Limit can never succeed, and Middleware must route that
// error to ErrorHandler like any other Take error — never LimitedHandler,
// and never calling next.
func TestMiddlewareCostExceedingLimitGoesToErrorHandler(t *testing.T) {
	l := newFixedClockLimiter(t, 5, time.Second, time.Unix(0, 0))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when cost exceeds the limiter's Limit")
	})

	limitedCalled := false
	var costErr error
	handler := Middleware(l, alwaysKey("k"), WithCost(6),
		WithLimitedHandler(func(http.ResponseWriter, *http.Request, Decision) { limitedCalled = true }),
		WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			costErr = err
			w.WriteHeader(http.StatusBadRequest)
		}),
	)(next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if limitedCalled {
		t.Fatal("an impossible cost must never be routed to limitedHandler")
	}
	if costErr == nil {
		t.Fatal("expected Take's cost-exceeds-limit error to reach errorHandler")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestErrKeyFuncEmptyMessage(t *testing.T) {
	if got := errEmptyKey.Error(); got == "" {
		t.Fatal("errEmptyKey.Error() must not be empty")
	}
}

func TestDefaultLimitedHandlerNeverWritesNegativeRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	defaultLimitedHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil), Decision{RetryAfter: -time.Second})

	if got := rec.Header().Get("Retry-After"); got != "0" {
		t.Fatalf("Retry-After = %q, want %q for a defensively-negative RetryAfter", got, "0")
	}
}

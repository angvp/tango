package ratelimit

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/angvp/tango"
)

// LimitedHandler responds to a request Limiter.Take rejected. It receives
// the computed Decision so a host can build its own headers/body from it
// (e.g. X-RateLimit-Remaining) instead of trusting the default response.
type LimitedHandler func(http.ResponseWriter, *http.Request, Decision)

// ErrorHandler responds when KeyFunc itself fails — distinct from a
// rejected Decision, so a key-extraction failure is never treated as (and
// never looks like, in logs or the response) "this caller is over budget."
type ErrorHandler func(http.ResponseWriter, *http.Request, error)

type middlewareConfig struct {
	limitedHandler LimitedHandler
	errorHandler   ErrorHandler
	cost           int
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithLimitedHandler overrides the default 429 JSON response for a
// rejected request.
func WithLimitedHandler(h LimitedHandler) MiddlewareOption {
	return func(c *middlewareConfig) { c.limitedHandler = h }
}

// WithErrorHandler overrides the default 400 JSON response for a KeyFunc
// failure.
func WithErrorHandler(h ErrorHandler) MiddlewareOption {
	return func(c *middlewareConfig) { c.errorHandler = h }
}

// WithCost sets how many tokens one request consumes. Defaults to 1.
func WithCost(cost int) MiddlewareOption {
	return func(c *middlewareConfig) { c.cost = cost }
}

// Middleware wraps next as ordinary tango.Middleware: composes at any of
// tanGO's existing global/group/route attachment tiers, and applies
// equally to a WebSocket upgrade route, since an upgrade is just a
// specially-shaped GET request hitting the same routing layer.
//
// key(r) is called first. A non-nil error, or an empty key with a nil
// error, both go to errorHandler — never treated as a rejected Decision.
// Otherwise limiter.Take is called; on Decision.Allowed == false,
// limitedHandler runs instead of next.
func Middleware(limiter *Limiter, key KeyFunc, opts ...MiddlewareOption) tango.Middleware {
	cfg := middlewareConfig{
		limitedHandler: defaultLimitedHandler,
		errorHandler:   defaultErrorHandler,
		cost:           1,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k, err := key(r)
			if err != nil {
				cfg.errorHandler(w, r, err)
				return
			}
			if k == "" {
				cfg.errorHandler(w, r, errEmptyKey)
				return
			}

			decision, err := limiter.Take(r.Context(), k, limiter.now(), cfg.cost)
			if err != nil {
				cfg.errorHandler(w, r, err)
				return
			}
			if !decision.Allowed {
				cfg.limitedHandler(w, r, decision)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

var errEmptyKey = errKeyFuncEmpty{}

type errKeyFuncEmpty struct{}

func (errKeyFuncEmpty) Error() string {
	return "tango ratelimit: key extractor returned an empty key"
}

// now returns the Limiter's configured clock — unexported, used internally
// by Middleware so a caller testing Middleware end-to-end can inject a
// fake clock via Options.Clock without needing to thread a now value
// through the middleware call itself. Direct Limiter.Take callers outside
// this package always pass an explicit now instead.
func (l *Limiter) now() time.Time {
	return l.clock()
}

func defaultLimitedHandler(w http.ResponseWriter, _ *http.Request, d Decision) {
	seconds := int(math.Ceil(d.RetryAfter.Seconds()))
	if seconds < 0 {
		seconds = 0
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
}

func defaultErrorHandler(w http.ResponseWriter, _ *http.Request, _ error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit key extraction failed"})
}

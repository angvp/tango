package admin

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/angvp/tango"
)

// loginRateLimitAttempts and loginRateLimitWindow bound how many failed
// login attempts a single source IP may make before being throttled — a
// single-process, in-memory counter, not an account lockout (see this
// milestone's Q4: rate limiting only, no lockout, no account-recovery
// story to design). This is a known v0.1 limitation: the counter resets
// on process restart and isn't shared across multiple server instances.
const (
	loginRateLimitAttempts = 5
	loginRateLimitWindow   = time.Minute
)

// loginRateLimiter tracks recent failed login attempts per key (source
// IP) so repeated brute-force attempts get throttled without needing an
// account-lockout state machine.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{attempts: make(map[string][]time.Time)}
}

// Allow reports whether key is currently under the failed-attempt
// threshold within the rate-limit window, pruning older attempts as it
// goes.
func (l *loginRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-loginRateLimitWindow)
	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	l.attempts[key] = kept

	return len(kept) < loginRateLimitAttempts
}

// RecordFailure records a failed login attempt for key.
func (l *loginRateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], time.Now())
}

// rateLimitKey derives the rate-limit key (source IP, port stripped) for
// a request.
func rateLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// tooManyLoginAttempts writes a generic 429 response for a throttled
// login attempt.
func tooManyLoginAttempts(ctx *tango.Context) error {
	ctx.ResponseWriter().Header().Set("Retry-After", "60")
	return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": "too many login attempts, try again later"})
}

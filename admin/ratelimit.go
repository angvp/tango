package admin

import (
	"net/http"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

// loginRateLimitAttempts and loginRateLimitWindow bound how many failed
// login attempts a single source IP may make before being throttled — a
// single-process, in-memory counter, not an account lockout (see this
// milestone's Q4: rate limiting only, no lockout, no account-recovery
// story to design). This is a known limitation: the counter resets
// on process restart and isn't shared across multiple server instances.
const (
	loginRateLimitAttempts = 5
	loginRateLimitWindow   = time.Minute
)

// tooManyLoginAttempts writes a generic 429 response for a throttled
// login attempt.
func tooManyLoginAttempts(ctx *tango.Context) error {
	ctx.ResponseWriter().Header().Set("Retry-After", "60")
	return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": i18n.T(ctx.Context(), "admin.error.too_many_login_attempts", "too many login attempts, try again later")})
}

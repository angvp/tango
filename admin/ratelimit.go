package admin

import (
	"time"
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

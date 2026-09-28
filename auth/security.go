// Package auth provides composable authentication primitives for tanGO
// applications.
package auth

import "github.com/angvp/tango/internal/security"

var (
	// DeriveCSRFToken derives a form-safe CSRF token from an arbitrary
	// secret, typically a session token.
	DeriveCSRFToken = security.DeriveCSRFToken
	// VerifyCSRFToken verifies a submitted CSRF token against a secret.
	VerifyCSRFToken = security.VerifyCSRFToken
	// SafeRedirect returns raw when it is safe for allowedPrefix, otherwise
	// fallback.
	SafeRedirect = security.SafeRedirect
	// IsSafeRedirect reports whether raw is safe for allowedPrefix.
	IsSafeRedirect = security.IsSafeRedirect
	// IsHTTPS reports whether the client connected over HTTPS, directly or
	// through a TLS-terminating proxy that sets X-Forwarded-Proto or
	// Forwarded. Use it for a cookie's Secure flag.
	IsHTTPS = security.IsHTTPS
)

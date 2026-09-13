package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// DeriveCSRFToken derives a form-safe CSRF token from a secret value such
// as a session token. The secret itself is never rendered into the form.
func DeriveCSRFToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// VerifyCSRFToken reports whether submitted matches the token derived from
// secret, using a constant-time comparison.
func VerifyCSRFToken(submitted string, secret string) bool {
	expected := DeriveCSRFToken(secret)
	return submitted != "" && subtle.ConstantTimeCompare([]byte(submitted), []byte(expected)) == 1
}

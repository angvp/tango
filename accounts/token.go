package accounts

import (
	"crypto/rand"
	"encoding/base64"
)

// randomToken returns a cryptographically random, URL-safe token — used
// for the pre-session CSRF cookie. Session tokens themselves come from
// auth.CreateSession, which generates its own.
func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

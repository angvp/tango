package security

import "strings"

// IsSafeRedirect reports whether raw is a same-application redirect target
// under allowedPrefix, rejecting absolute URLs and protocol-relative URLs.
func IsSafeRedirect(raw string, allowedPrefix string) bool {
	if allowedPrefix == "" || !strings.HasPrefix(raw, allowedPrefix) {
		return false
	}
	if strings.HasPrefix(raw, "//") || strings.Contains(raw, "://") {
		return false
	}
	return true
}

// SafeRedirect returns raw when it is safe for allowedPrefix, otherwise
// fallback.
func SafeRedirect(raw string, allowedPrefix string, fallback string) string {
	if !IsSafeRedirect(raw, allowedPrefix) {
		return fallback
	}
	return raw
}

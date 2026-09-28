package security

import (
	"net/http"
	"strings"
)

// IsHTTPS reports whether the client reached the app over HTTPS: either
// the connection itself is TLS, or a proxy that terminated TLS in front of
// the app says so in X-Forwarded-Proto or Forwarded (the first hop wins).
//
// It decides cookies' Secure flag, where trusting the headers is safe:
// a client that forges "https" only gets a cookie its own browser won't
// send back over plain HTTP. Don't use it for security decisions that a
// forged header could weaken. See docs/adr/0033.
func IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		first, _, _ := strings.Cut(proto, ",")
		return strings.EqualFold(strings.TrimSpace(first), "https")
	}
	return forwardedProto(r.Header.Get("Forwarded")) == "https"
}

// forwardedProto returns the lowercased proto of the first element of an
// RFC 7239 Forwarded header, or "".
func forwardedProto(header string) string {
	first, _, _ := strings.Cut(header, ",")
	for _, pair := range strings.Split(first, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.EqualFold(key, "proto") {
			return strings.ToLower(strings.Trim(value, `"`))
		}
	}
	return ""
}

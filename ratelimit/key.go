package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

// KeyFunc extracts a rate-limit key from a request. Caller-supplied:
// ratelimit never imports auth, auth/jwt, or accounts. A host may key on
// remote IP (see RemoteIPKey), a JWT claim, session identity, route
// identity, tenant identity, or anything else reachable from *http.Request.
type KeyFunc func(*http.Request) (string, error)

// RemoteIPKey returns a KeyFunc keyed on the request's remote IP.
//
// With no trustedProxies given, it always uses r.RemoteAddr's host (via
// net.SplitHostPort, falling back to the raw RemoteAddr value if it
// doesn't parse as host:port) — X-Forwarded-For and X-Real-IP are never
// consulted. This matches admin/accounts' existing login rate limiter,
// which has no proxy awareness at all.
//
// With trustedProxies given, X-Forwarded-For (its first, leftmost address)
// or X-Real-IP is honored instead, but only when the immediate peer
// (r.RemoteAddr's host) parses as an IP inside one of the given CIDRs.
// From an untrusted peer, those headers are ignored even if present —
// trusting them unconditionally would let any client spoof its own
// rate-limit key.
func RemoteIPKey(trustedProxies ...*net.IPNet) KeyFunc {
	return func(r *http.Request) (string, error) {
		peer := remoteAddrHost(r.RemoteAddr)

		if len(trustedProxies) > 0 && peerIsTrusted(peer, trustedProxies) {
			if forwarded := firstForwardedFor(r.Header.Get("X-Forwarded-For")); forwarded != "" {
				return forwarded, nil
			}
			if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
				return realIP, nil
			}
		}

		return peer, nil
	}
}

func remoteAddrHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func peerIsTrusted(peer string, trustedProxies []*net.IPNet) bool {
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	for _, cidr := range trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func firstForwardedFor(header string) string {
	first, _, _ := strings.Cut(header, ",")
	return strings.TrimSpace(first)
}

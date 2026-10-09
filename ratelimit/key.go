package ratelimit

import (
	"net"
	"net/http"

	"github.com/angvp/tango/internal/clientip"
)

// KeyFunc extracts a rate-limit key from a request. Caller-supplied:
// ratelimit never imports auth, auth/jwt, or accounts. A host may key on
// remote IP (see RemoteIPKey), a JWT claim, session identity, route
// identity, tenant identity, or anything else reachable from *http.Request.
type KeyFunc func(*http.Request) (string, error)

// RemoteIPKey returns a KeyFunc keyed on the request's client IP.
//
// With no trustedProxies given, it always uses r.RemoteAddr's host (via
// net.SplitHostPort, falling back to the raw RemoteAddr value if it
// doesn't parse as host:port); forwarded headers are never consulted. This
// matches admin/accounts' login rate limiter when it is not configured with
// trusted proxies.
//
// With trustedProxies given, and only when the immediate peer
// (r.RemoteAddr's host) is an IP inside one of the given networks,
// X-Forwarded-For is read from the right: addresses that are themselves in
// trustedProxies are skipped, and the first other address, the one your
// nearest trusted proxy saw, is the key. Whatever the client wrote to the
// left of it is ignored, so a client cannot pick its own key even through a
// proxy that appends to the header. An entry that is not a bare IP address, or
// a list with no usable entry, falls back to the peer's address. A valid
// address is returned in canonical form. X-Real-IP is never read. From an
// untrusted peer, headers are ignored even if present.
func RemoteIPKey(trustedProxies ...*net.IPNet) KeyFunc {
	return func(r *http.Request) (string, error) {
		return clientip.Resolve(r, trustedProxies), nil
	}
}

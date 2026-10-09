package security

import (
	"net"
	"net/http"

	"github.com/angvp/tango/ratelimit"
)

// ClientKey returns the function a failed-attempt limiter uses to name the
// client behind a request.
//
// With no trustedProxies it is the connection's remote host, port stripped,
// and no header is read: what these limiters have always done. With
// trustedProxies, X-Forwarded-For or X-Real-IP supplies the client address,
// but only for a request whose connection comes from inside one of those
// networks; a header from any other peer is ignored, so a client cannot pick
// its own key. The rules are ratelimit.RemoteIPKey's, so the two limiters
// cannot drift apart.
func ClientKey(trustedProxies []*net.IPNet) func(*http.Request) string {
	key := ratelimit.RemoteIPKey(trustedProxies...)
	return func(r *http.Request) string {
		// RemoteIPKey never fails; the error exists for other KeyFuncs.
		client, _ := key(r)
		return client
	}
}

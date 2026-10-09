package security

import (
	"net"
	"net/http"

	"github.com/angvp/tango/internal/clientip"
)

// ClientKey returns the function a failed-attempt limiter uses to name the
// client behind a request. With no trustedProxies it is the connection's
// remote host and no header is read, which is what these limiters have always
// done; with them, the rules are internal/clientip's, the same ones
// ratelimit.RemoteIPKey uses.
func ClientKey(trustedProxies []*net.IPNet) func(*http.Request) string {
	return func(r *http.Request) string { return clientip.Resolve(r, trustedProxies) }
}

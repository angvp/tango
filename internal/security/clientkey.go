package security

import (
	"net"
	"net/http"
	"strings"
)

// ClientKey returns the function a failed-attempt limiter uses to name the
// client behind a request.
//
// With no trustedProxies it is the connection's remote host, port stripped,
// and no header is read: what these limiters have always done.
//
// With trustedProxies, and only for a request whose connection comes from
// inside one of those networks, the client is read from X-Forwarded-For. The
// list is walked from the right, skipping addresses that are themselves
// trusted proxies, and the first other address is the client. That is the
// address the nearest trusted proxy actually saw; whatever the client wrote
// further left is never used, so it cannot pick its own key even when the
// proxy appends to the header rather than replacing it. An entry that is not
// a bare IP address ends the walk, and a request with no usable list is keyed
// by the connection address. The address is returned in its canonical form,
// so one client is one key however it is written. No other header is read.
func ClientKey(trustedProxies []*net.IPNet) func(*http.Request) string {
	return func(r *http.Request) string {
		peer := remoteHost(r.RemoteAddr)
		if len(trustedProxies) == 0 || !within(net.ParseIP(peer), trustedProxies) {
			return peer
		}
		hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			hop := net.ParseIP(strings.TrimSpace(hops[i]))
			if hop == nil {
				return peer
			}
			if !within(hop, trustedProxies) {
				return hop.String()
			}
		}
		return peer
	}
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func within(ip net.IP, networks []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

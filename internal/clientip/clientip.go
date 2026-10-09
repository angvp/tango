// Package clientip names the client behind an HTTP request, for the
// limiters that count per client. It is the one place that decides whether a
// forwarded header may be believed.
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Resolve returns the key for r's client.
//
// With no trusted networks it is the connection's remote host, port stripped,
// and no header is read.
//
// With trusted networks, and only for a request whose connection comes from
// inside one of them, the client is read from X-Forwarded-For. The list is
// walked from the right, skipping addresses that are themselves trusted, and
// the first other address is the client: the address the nearest trusted
// proxy actually saw. Whatever the client wrote further left is never used,
// so it cannot pick its own key even when the proxy appends to the header
// rather than replacing it. An entry that is not a bare IP address ends the
// walk, and a request with no usable list is keyed by the connection
// address. A valid address is returned in canonical form, so one client is
// one key however it is written. No other header is read.
func Resolve(r *http.Request, trusted []*net.IPNet) string {
	peer := remoteHost(r.RemoteAddr)
	if !within(net.ParseIP(peer), trusted) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := net.ParseIP(strings.TrimSpace(hops[i]))
		if hop == nil {
			return peer
		}
		if !within(hop, trusted) {
			return hop.String()
		}
	}
	return peer
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// within reports whether ip is inside any network. A nil ip or nil network
// is never a match.
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

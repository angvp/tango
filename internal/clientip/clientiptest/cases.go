// Package clientiptest holds the cases every user of internal/clientip is
// tested against, so the public ratelimit.RemoteIPKey and the login limiters'
// resolver cannot disagree about what a request's client is.
package clientiptest

// Case is one request and the key a resolver must give it.
type Case struct {
	Name string
	// Trusted is the trusted-proxy networks in CIDR form.
	Trusted []string
	// Peer is the request's RemoteAddr.
	Peer string
	// ForwardedFor holds the X-Forwarded-For header lines, in order.
	ForwardedFor []string
	// RealIP is the X-Real-IP header, which no resolver may read.
	RealIP string
	Want   string
}

var private = []string{"10.0.0.0/8"}

// Cases is the shared table.
var Cases = []Case{
	{Name: "no trusted proxies reads no header", Peer: "203.0.113.5:1234", ForwardedFor: []string{"198.51.100.9"}, RealIP: "198.51.100.9", Want: "203.0.113.5"},
	{Name: "no trusted proxies, peer without a port", Peer: "no-port-here", Want: "no-port-here"},
	{Name: "untrusted peer's header is ignored", Trusted: private, Peer: "203.0.113.5:1234", ForwardedFor: []string{"6.6.6.6"}, Want: "203.0.113.5"},
	{Name: "peer that is not an IP is never trusted", Trusted: private, Peer: "not-an-ip:1234", ForwardedFor: []string{"6.6.6.6"}, Want: "not-an-ip"},
	{Name: "trusted peer: the client the proxy saw", Trusted: private, Peer: "10.0.0.1:1234", ForwardedFor: []string{"198.51.100.9"}, Want: "198.51.100.9"},
	{Name: "an appending proxy: the client's own claim is ignored", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"1.1.1.1, 203.0.113.7"}, Want: "203.0.113.7"},
	{Name: "the client's claim cannot change the key", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"9.9.9.9, 8.8.8.8, 203.0.113.7"}, Want: "203.0.113.7"},
	{Name: "trusted hops are skipped from the right", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"203.0.113.7, 10.0.0.9"}, Want: "203.0.113.7"},
	{Name: "several header lines are one list", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"1.1.1.1", "203.0.113.7, 10.0.0.9"}, Want: "203.0.113.7"},
	{Name: "no header falls back to the peer", Trusted: private, Peer: "10.0.0.5:80", Want: "10.0.0.5"},
	{Name: "only trusted hops falls back to the peer", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"10.0.0.7, 10.0.0.9"}, Want: "10.0.0.5"},
	{Name: "garbage falls back to the peer", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"not-an-ip"}, Want: "10.0.0.5"},
	{Name: "garbage on the right falls back to the peer", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"203.0.113.7, junk"}, Want: "10.0.0.5"},
	{Name: "an empty entry falls back to the peer", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{", 203.0.113.7,"}, Want: "10.0.0.5"},
	{Name: "an address with a port is not a hop", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"203.0.113.7:4455"}, Want: "10.0.0.5"},
	{Name: "X-Real-IP is never read", Trusted: private, Peer: "10.0.0.5:80", RealIP: "203.0.113.7", Want: "10.0.0.5"},
	{Name: "an empty entry on the left does not matter, and X-Real-IP is not consulted", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{", 1.2.3.4"}, RealIP: "203.0.113.7", Want: "1.2.3.4"},
	{Name: "IPv6 is canonical", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"2001:DB8:0:0:0:0:0:1"}, Want: "2001:db8::1"},
	{Name: "an IPv4-mapped address is the IPv4 address", Trusted: private, Peer: "10.0.0.5:80", ForwardedFor: []string{"::ffff:203.0.113.7"}, Want: "203.0.113.7"},
}

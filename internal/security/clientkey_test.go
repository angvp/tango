package security

import (
	"net"
	"net/http"
	"testing"
)

func TestClientKeyWithoutProxiesIsTheRemoteHostAndReadsNoHeader(t *testing.T) {
	key := ClientKey(nil)
	r := &http.Request{RemoteAddr: "192.0.2.1:5555", Header: http.Header{"X-Forwarded-For": {"203.0.113.9"}}}
	if got := key(r); got != "192.0.2.1" {
		t.Fatalf("key = %q, want the remote host 192.0.2.1", got)
	}
}

func TestClientKeyFallsBackToTheRawRemoteAddr(t *testing.T) {
	// A RemoteAddr without a port is returned as given.
	if got := ClientKey(nil)(&http.Request{RemoteAddr: "no-port-here"}); got != "no-port-here" {
		t.Fatalf("key = %q, want the raw RemoteAddr", got)
	}
}

func TestClientKeyReadsTheForwardedAddressOnlyFromATrustedPeer(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("10.0.0.0/8")
	key := ClientKey([]*net.IPNet{trusted})
	forwarded := http.Header{"X-Forwarded-For": {"203.0.113.9"}}

	if got := key(&http.Request{RemoteAddr: "10.1.2.3:80", Header: forwarded}); got != "203.0.113.9" {
		t.Fatalf("trusted peer: key = %q, want the forwarded client", got)
	}
	if got := key(&http.Request{RemoteAddr: "198.51.100.1:80", Header: forwarded}); got != "198.51.100.1" {
		t.Fatalf("untrusted peer: key = %q, want its own address", got)
	}
}

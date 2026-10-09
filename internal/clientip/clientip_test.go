package clientip

import (
	"net"
	"net/http"
	"testing"

	"github.com/angvp/tango/internal/clientip/clientiptest"
)

func TestResolveAgreesWithTheSharedCases(t *testing.T) {
	for _, tc := range clientiptest.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			var trusted []*net.IPNet
			for _, cidr := range tc.Trusted {
				_, n, err := net.ParseCIDR(cidr)
				if err != nil {
					t.Fatal(err)
				}
				trusted = append(trusted, n)
			}
			r := &http.Request{RemoteAddr: tc.Peer, Header: http.Header{}}
			for _, line := range tc.ForwardedFor {
				r.Header.Add("X-Forwarded-For", line)
			}
			if tc.RealIP != "" {
				r.Header.Set("X-Real-IP", tc.RealIP)
			}
			if got := Resolve(r, trusted); got != tc.Want {
				t.Fatalf("key = %q, want %q", got, tc.Want)
			}
		})
	}
}

func TestResolveIgnoresNilNetworks(t *testing.T) {
	_, ten, _ := net.ParseCIDR("10.0.0.0/8")
	r := &http.Request{RemoteAddr: "10.0.0.1:1", Header: http.Header{"X-Forwarded-For": {"198.51.100.9"}}}
	if got := Resolve(r, []*net.IPNet{nil, ten, nil}); got != "198.51.100.9" {
		t.Fatalf("key = %q, want the forwarded client despite nil entries", got)
	}
	if got := Resolve(r, []*net.IPNet{nil, nil}); got != "10.0.0.1" {
		t.Fatalf("key = %q, want the peer: only nil networks trust nothing", got)
	}
}

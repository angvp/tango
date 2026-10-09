package ratelimit

import (
	"net"
	"net/http"
	"testing"

	"github.com/angvp/tango/internal/clientip/clientiptest"
)

// The same cases the login limiters' resolver is tested against.
func TestRemoteIPKeyAgreesWithTheSharedCases(t *testing.T) {
	for _, tc := range clientiptest.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			var trusted []*net.IPNet
			for _, cidr := range tc.Trusted {
				trusted = append(trusted, mustCIDR(t, cidr))
			}
			r := &http.Request{RemoteAddr: tc.Peer, Header: http.Header{}}
			for _, line := range tc.ForwardedFor {
				r.Header.Add("X-Forwarded-For", line)
			}
			if tc.RealIP != "" {
				r.Header.Set("X-Real-IP", tc.RealIP)
			}
			got, err := RemoteIPKey(trusted...)(r)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.Want {
				t.Fatalf("key = %q, want %q", got, tc.Want)
			}
		})
	}
}

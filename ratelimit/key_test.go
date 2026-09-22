package ratelimit

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, cidr, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", s, err)
	}
	return cidr
}

func TestRemoteIPKeyDefaultIgnoresForwardedHeaders(t *testing.T) {
	key := RemoteIPKey()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	req.Header.Set("X-Real-IP", "198.51.100.9")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "203.0.113.5" {
		t.Fatalf("key = %q, want the real peer address, not a forwarded header", got)
	}
}

func TestRemoteIPKeyTrustedProxyHonorsForwardedFor(t *testing.T) {
	trusted := mustCIDR(t, "10.0.0.0/8")
	key := RemoteIPKey(trusted)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345" // inside the trusted CIDR
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "198.51.100.9" {
		t.Fatalf("key = %q, want the leftmost X-Forwarded-For address from a trusted peer", got)
	}
}

func TestRemoteIPKeyUntrustedPeerIgnoresForgedHeader(t *testing.T) {
	trusted := mustCIDR(t, "10.0.0.0/8")
	key := RemoteIPKey(trusted)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:12345" // NOT inside the trusted CIDR
	req.Header.Set("X-Forwarded-For", "6.6.6.6")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "203.0.113.5" {
		t.Fatalf("key = %q, want the real peer address — an untrusted peer must not be able to spoof its key via X-Forwarded-For", got)
	}
}

func TestRemoteIPKeyTrustedProxyRealIPFallback(t *testing.T) {
	trusted := mustCIDR(t, "10.0.0.0/8")
	key := RemoteIPKey(trusted)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "198.51.100.9")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "198.51.100.9" {
		t.Fatalf("key = %q, want X-Real-IP honored from a trusted peer with no X-Forwarded-For", got)
	}
}

func TestRemoteIPKeyMalformedRemoteAddrFallsBackToRawValue(t *testing.T) {
	key := RemoteIPKey()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-a-host-port"

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "not-a-host-port" {
		t.Fatalf("key = %q, want the raw RemoteAddr as a fallback", got)
	}
}

func TestRemoteIPKeyTrustedProxiesWithUnparseablePeerIgnoresForwardedHeader(t *testing.T) {
	trusted := mustCIDR(t, "10.0.0.0/8")
	key := RemoteIPKey(trusted)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// A peer address that fails net.ParseIP (not a malformed host:port —
	// remoteAddrHost falls back to the raw string, which then must not be
	// treated as trusted by peerIsTrusted).
	req.RemoteAddr = "not-an-ip:12345"
	req.Header.Set("X-Forwarded-For", "6.6.6.6")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "not-an-ip" {
		t.Fatalf("key = %q, want the raw peer host, not the forwarded header, when the peer isn't a parseable IP", got)
	}
}

func TestRemoteIPKeyNilTrustedCIDREntryIsIgnoredSafely(t *testing.T) {
	trusted := mustCIDR(t, "10.0.0.0/8")
	// A nil entry mixed in with valid ones must never panic, and must
	// simply be skipped — the valid entries still work normally.
	key := RemoteIPKey(nil, trusted, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "198.51.100.9" {
		t.Fatalf("key = %q, want the forwarded address honored from the valid trusted CIDR despite the nil entries", got)
	}
}

func TestRemoteIPKeyOnlyNilTrustedCIDRsNeverTrusts(t *testing.T) {
	key := RemoteIPKey(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")

	got, err := key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if got != "10.0.0.1" {
		t.Fatalf("key = %q, want the real peer address — an all-nil trustedProxies list must never trust anything", got)
	}
}

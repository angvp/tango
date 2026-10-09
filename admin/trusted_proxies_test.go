package admin_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

func proxyCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	return network
}

func buildProxyTestHandler(t *testing.T, opts ...admin.Option) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	_, store := migratedAdminDB(t, registry)
	if err := registry.Register(admin.New(store, opts...)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

// failedLoginFrom posts a wrong password as a request whose connection comes
// from peer and, when forwardedFor is not empty, carries that X-Forwarded-For.
func failedLoginFrom(t *testing.T, handler http.Handler, peer, forwardedFor string) int {
	t.Helper()
	csrf := fetchLoginCSRF(t, handler)
	form := url.Values{"username": {"admin"}, "password": {"wrong"}, "csrf_token": {csrf.Value}}
	request := httptest.NewRequest(http.MethodPost, "/admin/login/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(csrf)
	request.RemoteAddr = peer + ":4321"
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code
}

func TestLoginLimiterIgnoresForwardedForByDefault(t *testing.T) {
	handler := buildProxyTestHandler(t)

	// Six different claimed clients behind one proxy address: without
	// configuration they all count against the proxy, as they always have.
	var last int
	for i := 0; i < 6; i++ {
		last = failedLoginFrom(t, handler, "10.0.0.1", "203.0.113."+string(rune('1'+i)))
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: X-Forwarded-For must not be trusted by default", last)
	}
}

func TestLoginLimiterCountsEachClientBehindATrustedProxy(t *testing.T) {
	handler := buildProxyTestHandler(t, admin.WithTrustedProxies(proxyCIDR(t, "10.0.0.0/8")))

	var last int
	for i := 0; i < 6; i++ {
		last = failedLoginFrom(t, handler, "10.0.0.1", "203.0.113.7")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("sixth failure from one client = %d, want 429", last)
	}
	if other := failedLoginFrom(t, handler, "10.0.0.1", "203.0.113.8"); other == http.StatusTooManyRequests {
		t.Fatal("a different client behind the same trusted proxy was throttled with the first")
	}
}

func TestLoginLimiterIgnoresForwardedForFromAnUntrustedPeer(t *testing.T) {
	handler := buildProxyTestHandler(t, admin.WithTrustedProxies(proxyCIDR(t, "10.0.0.0/8")))

	// A client outside the trusted range forges a new address each time to
	// dodge the limit; the header is ignored, so it is still one bucket.
	var last int
	for i := 0; i < 6; i++ {
		last = failedLoginFrom(t, handler, "198.51.100.9", "203.0.113."+string(rune('1'+i)))
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: a forged X-Forwarded-For from an untrusted peer must not evade the limit", last)
	}
}

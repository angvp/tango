package accounts_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
)

func proxyCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	return network
}

// failedLoginFrom posts a wrong password as a request whose connection comes
// from peer and, when forwardedFor is not empty, carries that X-Forwarded-For.
func failedLoginFrom(t *testing.T, handler http.Handler, peer, forwardedFor string) int {
	t.Helper()
	csrf := fetchLoginCSRF(t, handler)
	form := url.Values{"email": {"nobody@example.com"}, "password": {"wrong"}, "csrf_token": {csrf.Value}}
	request := httptest.NewRequest(http.MethodPost, "/accounts/login/", strings.NewReader(form.Encode()))
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
	handler, _ := buildRegisterTestHandler(t)

	var last int
	for i := 0; i < 6; i++ {
		last = failedLoginFrom(t, handler, "10.0.0.1", "203.0.113."+string(rune('1'+i)))
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: X-Forwarded-For must not be trusted by default", last)
	}
}

func TestLoginLimiterCountsEachClientBehindATrustedProxy(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t, accounts.WithTrustedProxies(proxyCIDR(t, "10.0.0.0/8")))

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
	handler, _ := buildRegisterTestHandler(t, accounts.WithTrustedProxies(proxyCIDR(t, "10.0.0.0/8")))

	var last int
	for i := 0; i < 6; i++ {
		last = failedLoginFrom(t, handler, "198.51.100.9", "203.0.113."+string(rune('1'+i)))
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: a forged X-Forwarded-For from an untrusted peer must not evade the limit", last)
	}
}

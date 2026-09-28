package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Behind a proxy that terminates TLS the app sees plain HTTP, so the
// cookie's Secure flag has to come from the proxy's X-Forwarded-Proto.
func TestLoginCSRFCookieIsSecureBehindTLSProxy(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	for _, tt := range []struct {
		proto string
		want  bool
	}{{"https", true}, {"", false}} {
		request := httptest.NewRequest(http.MethodGet, "/admin/login/", nil)
		if tt.proto != "" {
			request.Header.Set("X-Forwarded-Proto", tt.proto)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		var found bool
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == "tango_admin_login_csrf" {
				found = true
				if cookie.Secure != tt.want {
					t.Fatalf("X-Forwarded-Proto %q: Secure = %v, want %v", tt.proto, cookie.Secure, tt.want)
				}
			}
		}
		if !found {
			t.Fatal("GET /admin/login/ did not set the login CSRF cookie")
		}
	}
}

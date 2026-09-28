package accounts_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Behind a proxy that terminates TLS the app sees plain HTTP, so the
// cookies' Secure flag has to come from the proxy's X-Forwarded-Proto.
func TestCookiesAreSecureBehindTLSProxy(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/accounts/register/", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	csrf := findCookie(response, "tango_account_pre_session_csrf")
	if csrf == nil || !csrf.Secure {
		t.Fatalf("pre-session CSRF cookie = %+v, want Secure behind an https proxy", csrf)
	}

	form := url.Values{"email": {"ada@example.test"}, "password": {"correct-password"}, "csrf_token": {csrf.Value}}
	request = httptest.NewRequest(http.MethodPost, "/accounts/register/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(csrf)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	session := findCookie(response, "tango_account_session")
	if session == nil || !session.Secure {
		t.Fatalf("session cookie = %+v, want Secure behind an https proxy", session)
	}
}

func TestCookiesAreNotSecureOverPlainHTTP(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"ada@example.test"}, "password": {"correct-password"}})
	session := findCookie(response, "tango_account_session")
	if session == nil || session.Secure {
		t.Fatalf("session cookie = %+v, want a non-Secure cookie over plain HTTP", session)
	}
}

func findCookie(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

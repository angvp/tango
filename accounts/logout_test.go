package accounts_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestLogoutDeletesSessionAndCookieNoLongerAuthenticates(t *testing.T) {
	handler, _ := buildProtectedTestHandler(t)
	registerAccount(t, handler, "rick@example.com", "correct-password")
	loginResponse := postLogin(t, handler, url.Values{"email": {"rick@example.com"}, "password": {"correct-password"}})

	var sessionCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set a session cookie")
	}

	if before := performProtectedRequest(t, handler, sessionCookie); before.Code != http.StatusOK {
		t.Fatalf("protected route before logout = %d, want %d", before.Code, http.StatusOK)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/accounts/logout/", nil)
	logoutRequest.AddCookie(sessionCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)

	if logoutResponse.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", logoutResponse.Code, http.StatusFound)
	}
	if got := logoutResponse.Header().Get("Location"); got != "/accounts/login/" {
		t.Fatalf("logout Location = %q, want %q", got, "/accounts/login/")
	}

	after := performProtectedRequest(t, handler, sessionCookie)
	if after.Code != http.StatusFound {
		t.Fatalf("protected route after logout = %d, want %d (redirect, session deleted)", after.Code, http.StatusFound)
	}
}

func TestLogoutHasNoGetRoute(t *testing.T) {
	handler, _ := buildProtectedTestHandler(t)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/accounts/logout/", nil))

	if response.Code == http.StatusOK || response.Code == http.StatusFound {
		t.Fatalf("GET /accounts/logout/ status = %d, want it rejected (no GET route)", response.Code)
	}
}

func TestLogoutDoesNotAffectOtherSessionsForSameAccount(t *testing.T) {
	handler, _ := buildProtectedTestHandler(t)
	registerAccount(t, handler, "sara@example.com", "correct-password")

	firstLogin := postLogin(t, handler, url.Values{"email": {"sara@example.com"}, "password": {"correct-password"}})
	secondLogin := postLogin(t, handler, url.Values{"email": {"sara@example.com"}, "password": {"correct-password"}})

	var firstCookie, secondCookie *http.Cookie
	for _, cookie := range firstLogin.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			firstCookie = cookie
		}
	}
	for _, cookie := range secondLogin.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			secondCookie = cookie
		}
	}
	if firstCookie == nil || secondCookie == nil {
		t.Fatal("both logins should have set a session cookie")
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/accounts/logout/", nil)
	logoutRequest.AddCookie(firstCookie)
	handler.ServeHTTP(httptest.NewRecorder(), logoutRequest)

	// The second session, from a separate login, must still work.
	stillWorks := performProtectedRequest(t, handler, secondCookie)
	if stillWorks.Code != http.StatusOK {
		t.Fatalf("second session after first session's logout = %d, want %d (untouched)", stillWorks.Code, http.StatusOK)
	}
}

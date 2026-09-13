package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fetchLoginCSRF performs a GET /admin/login/ and returns the resulting
// login-CSRF cookie, which every login test needs before it can POST.
func fetchLoginCSRF(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/admin/login/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_admin_login_csrf" {
			return cookie
		}
	}
	t.Fatal("GET /admin/login/ did not set a login CSRF cookie")
	return nil
}

// postLogin performs a real, CSRF-correct POST /admin/login/ with the
// given form values (username/password/next), returning the response.
func postLogin(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	csrfCookie := fetchLoginCSRF(t, handler)

	submitted := url.Values{}
	for key, values := range form {
		submitted[key] = values
	}
	submitted.Set("csrf_token", csrfCookie.Value)

	request := httptest.NewRequest(http.MethodPost, "/admin/login/", strings.NewReader(submitted.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

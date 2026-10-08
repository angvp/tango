package accounts_test

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail/mailtest"
)

// This file pins the accounts HTTP contract docs/compatibility.md promises,
// so the page can't drift from what the package does. Covered: the three
// endpoints and their methods, a rejected CSRF token answering 403 with a
// JSON {"error": …} object, and rate limiting answering 429 with one and a
// Retry-After header. Everything else accounts answers is HTML, not covered
// beyond its status.

// assertJSONError checks response is status with a JSON object carrying a
// non-empty "error" string. The string's wording is not part of the
// contract.
func assertJSONError(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}
	if mediaType(response) != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", response.Body.String(), err)
	}
	if message, ok := body["error"].(string); !ok || message == "" {
		t.Fatalf("body %q has no non-empty \"error\" string", response.Body.String())
	}
}

func mediaType(response *httptest.ResponseRecorder) string {
	mediaType, _, _ := mime.ParseMediaType(response.Header().Get("Content-Type"))
	return mediaType
}

func TestDocumentedEndpointsAnswerTheirMethods(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)
	csrfCookie := fetchLoginCSRF(t, handler)
	tests := []struct{ method, path string }{
		{http.MethodGet, "/accounts/register/"},
		{http.MethodPost, "/accounts/register/"},
		{http.MethodGet, "/accounts/login/"},
		{http.MethodPost, "/accounts/login/"},
		{http.MethodPost, "/accounts/logout/"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(url.Values{"csrf_token": {csrfCookie.Value}}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(csrfCookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusNotFound || response.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s status = %d, want the endpoint to answer", tt.method, tt.path, response.Code)
			}
		})
	}
}

func TestDocumentedMailEndpointsAnswerTheirMethods(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	tests := []struct{ method, path string }{
		{http.MethodGet, "/accounts/password-reset/"},
		{http.MethodPost, "/accounts/password-reset/"},
		{http.MethodGet, "/accounts/password-reset/confirm/?token=x"},
		{http.MethodPost, "/accounts/password-reset/confirm/?token=x"},
		{http.MethodGet, "/accounts/verify/?token=x"},
		{http.MethodPost, "/accounts/verify/?token=x"},
		{http.MethodPost, "/accounts/verify/resend/"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			site.handler.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code == http.StatusNotFound || response.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s status = %d, want the endpoint to answer", tt.method, tt.path, response.Code)
			}
		})
	}
}

func TestRejectedCSRFTokenIsAJSONError(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)
	for _, path := range []string{"/accounts/register/", "/accounts/login/"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("email=x@example.com&password=correct-password"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertJSONError(t, response, http.StatusForbidden)
		})
	}
}

func TestRateLimitingIsAJSONErrorWithRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		post func(t *testing.T, handler http.Handler) *httptest.ResponseRecorder
	}{
		{"login", func(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
			return postLogin(t, handler, url.Values{"email": {"penny@example.com"}, "password": {"wrong-password"}})
		}},
		{"register", func(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
			return postRegister(t, handler, url.Values{"email": {"eve@example.com"}, "password": {"short"}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := buildRegisterTestHandler(t)
			var last *httptest.ResponseRecorder
			for i := 0; i < 6; i++ {
				last = tt.post(t, handler)
			}
			assertJSONError(t, last, http.StatusTooManyRequests)
			if last.Header().Get("Retry-After") == "" {
				t.Fatal("429 has no Retry-After header")
			}
		})
	}
}

// TestClosedRegistrationIsAnHTMLPage keeps the page from promising JSON for
// a 403 accounts answers with HTML.
func TestClosedRegistrationIsAnHTMLPage(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t, accounts.WithSignupDisabled())
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, "/accounts/register/", nil))
			if response.Code != http.StatusForbidden || mediaType(response) != "text/html" {
				t.Fatalf("%s status = %d, Content-Type = %q; want 403 text/html", method, response.Code, response.Header().Get("Content-Type"))
			}
		})
	}
}

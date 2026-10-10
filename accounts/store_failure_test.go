package accounts_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
)

// leaks lists words a sanitized 500 must not contain: driver and SQL detail.
var leaks = []string{"sql", "closed", "database", "SELECT", "INSERT", "UPDATE"}

func assertGeneric500(t *testing.T, name string, body string, code int) {
	t.Helper()
	if code != http.StatusInternalServerError {
		t.Errorf("%s: status %d, want 500 (a store failure); body %s", name, code, body)
		return
	}
	for _, word := range leaks {
		if strings.Contains(strings.ToLower(body), strings.ToLower(word)) {
			t.Errorf("%s: a 500 leaked %q: %s", name, word, body)
		}
	}
}

func TestEveryJSONEndpointAnswersAGeneric500WhenTheStoreFails(t *testing.T) {
	account := func(site *mailSite) {
		site.createAccount("member@example.com", true)
	}
	tests := []struct {
		name, path, body string
		headers          map[string]string
	}{
		{"register", "/accounts/api/register/", `{"email":"new@example.com","password":"correct-horse"}`, nil},
		{"login", "/accounts/api/login/", `{"identifier":"member@example.com","password":"old-password"}`, nil},
		{"confirm a reset", "/accounts/api/password-reset/confirm/", `{"token":"` + strings.Repeat("a", 43) + `","password":"correct-horse-2"}`, nil},
		{"verify", "/accounts/api/verify/", `{"token":"` + strings.Repeat("a", 43) + `"}`, nil},
		{"resend a verification", "/accounts/api/verify/resend/", `{}`, map[string]string{"Authorization": "Bearer tok-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newJSONSite(t, &fakeAuth{})
			account(site)
			if err := site.db.Close(); err != nil {
				t.Fatal(err)
			}
			response := site.postJSON(tt.path, tt.body, func(r *http.Request) {
				for k, v := range tt.headers {
					r.Header.Set(k, v)
				}
			})
			assertGeneric500(t, tt.name, response.Body.String(), response.Code)
		})
	}
}

func TestAnAuthenticatorFailureOnResendIsAGeneric500(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{authErr: http.ErrServerClosed})
	response := site.postJSON("/accounts/api/verify/resend/", `{}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok-1") })
	assertGeneric500(t, "resend with a failing authenticator", response.Body.String(), response.Code)
}

func TestEveryFormPageAnswersA500WhenTheStoreFails(t *testing.T) {
	tests := []struct {
		name, path string
		form       url.Values
	}{
		{"login", "/accounts/login/", url.Values{"email": {"member@example.com"}, "password": {"old-password"}}},
		{"register", "/accounts/register/", url.Values{"email": {"new@example.com"}, "password": {"correct-horse"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}})
			site.createAccount("member@example.com", true)
			if err := site.db.Close(); err != nil {
				t.Fatal(err)
			}
			response := site.postForm(tt.path, tt.form)
			assertGeneric500(t, tt.name, response.Body.String(), response.Code)
		})
	}
}

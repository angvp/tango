package accounts_test

import (
	"net/http"
	"net/http/httptest"
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

// dropTable removes a table the way a broken deployment or a lost
// migration would, leaving the rest of the schema usable.
func dropTable(t *testing.T, site *mailSite, table string) {
	t.Helper()
	if _, err := site.db.Exec(`DROP TABLE ` + table); err != nil {
		t.Fatal(err)
	}
}

func TestTokenLinksAnswerA500WhenTheTokenTableIsGone(t *testing.T) {
	for _, path := range []string{"/accounts/password-reset/confirm/", "/accounts/verify/"} {
		t.Run(path, func(t *testing.T) {
			site := newJSONSite(t, &fakeAuth{})
			site.createAccount("member@example.com", true)
			dropTable(t, site, "account_token")
			link := path + "?token=" + strings.Repeat("a", 43)

			response := site.get(link)
			assertGeneric500(t, "GET "+link, response.Body.String(), response.Code)
			response = site.postForm(link, url.Values{"password": {"correct-horse-2"}})
			assertGeneric500(t, "POST "+link, response.Body.String(), response.Code)
		})
	}
}

func TestIssuingATokenFailsCleanlyWhenTheTokenTableIsGone(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("member@example.com", true)
	dropTable(t, site, "account_token")
	// A reset request is always the same 202; the failure is the outbox's to
	// retry, never something a client can learn the account's existence from.
	response := site.postJSON("/accounts/api/password-reset/", `{"email":"member@example.com"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s; want the same 202 as for any address", response.Code, response.Body.String())
	}
	if got := len(site.messages()); got != 0 {
		t.Fatalf("%d message(s) sent without a token to put in them", got)
	}
}

func TestACompletedResetThatCannotClearSessionsIsA500NotASilentSuccess(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("member@example.com", true)
	token := site.resetToken("member@example.com")
	dropTable(t, site, "account_session")

	response := site.postJSON("/accounts/api/password-reset/confirm/", `{"token":"`+token+`","password":"correct-horse-2"}`)
	assertGeneric500(t, "reset confirm without a session table", response.Body.String(), response.Code)
}

func TestSessionStoreFailuresAreA500NotALoginRedirectOrALogout(t *testing.T) {
	setup := func(t *testing.T) (*mailSite, *http.Cookie) {
		site := newJSONSite(t, &fakeAuth{})
		site.createAccount("member@example.com", true)
		cookie := site.logIn("member@example.com", "old-password")
		if cookie == nil {
			t.Fatal("no session cookie after logging in")
		}
		return site, cookie
	}
	t.Run("logging in without a session table", func(t *testing.T) {
		site, _ := setup(t)
		dropTable(t, site, "account_session")
		response := site.postForm("/accounts/login/", url.Values{"email": {"member@example.com"}, "password": {"old-password"}}, func(r *http.Request) { r.RemoteAddr = "198.51.100.9:1" })
		assertGeneric500(t, "login", response.Body.String(), response.Code)
	})
	t.Run("logging out without a session table", func(t *testing.T) {
		site, cookie := setup(t)
		dropTable(t, site, "account_session")
		request := httptest.NewRequest(http.MethodPost, "/accounts/logout/", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		site.handler.ServeHTTP(response, request)
		assertGeneric500(t, "logout", response.Body.String(), response.Code)
	})
	t.Run("a protected page without a session table", func(t *testing.T) {
		site, cookie := setup(t)
		dropTable(t, site, "account_session")
		response := site.get("/private/", cookie)
		assertGeneric500(t, "protected page", response.Body.String(), response.Code)
	})
	t.Run("a protected page without an account table", func(t *testing.T) {
		site, cookie := setup(t)
		// Renamed rather than dropped: other tables point at it.
		if _, err := site.db.Exec(`ALTER TABLE account RENAME TO account_gone`); err != nil {
			t.Fatal(err)
		}
		response := site.get("/private/", cookie)
		assertGeneric500(t, "protected page", response.Body.String(), response.Code)
	})
}

func TestRegisteringWithoutASessionTableIsA500AndNeverAHalfSignedInPage(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	dropTable(t, site, "account_session")
	response := site.postForm("/accounts/register/", url.Values{"email": {"new@example.com"}, "password": {"correct-horse"}})
	assertGeneric500(t, "register", response.Body.String(), response.Code)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_session" && cookie.Value != "" {
			t.Fatalf("a session cookie %q was set for a session that was never stored", cookie.Name)
		}
	}
}

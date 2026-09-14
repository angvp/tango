package accounts_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
)

// buildLoginTestHandler is buildRegisterTestHandler plus the raw *sql.DB,
// for the one test here that needs to flip Active directly — a state
// accounts exposes no public API to reach yet (no admin-registration
// helper, no CLI, per this milestone's own non-goals).
func buildLoginTestHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if _, err := sqlDB.Exec(`CREATE TABLE account (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		active BOOLEAN NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create account table: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE account_session (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL,
		expires_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create account_session table: %v", err)
	}

	store := db.NewStore(sqlDB, db.SQLite)
	registry := tango.NewRegistry()
	if err := registry.Register(accounts.New(store)); err != nil {
		t.Fatalf("register accounts app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB
}

// fetchLoginCSRF performs a GET /accounts/login/ and returns the resulting
// pre-session CSRF cookie.
func fetchLoginCSRF(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/accounts/login/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_pre_session_csrf" {
			return cookie
		}
	}
	t.Fatal("GET /accounts/login/ did not set a pre-session CSRF cookie")
	return nil
}

func postLogin(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	csrfCookie := fetchLoginCSRF(t, handler)

	submitted := url.Values{}
	for key, values := range form {
		submitted[key] = values
	}
	submitted.Set("csrf_token", csrfCookie.Value)

	request := httptest.NewRequest(http.MethodPost, "/accounts/login/", strings.NewReader(submitted.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// registerAccount is a small test helper that registers a real account via
// the actual HTTP endpoint, for tests that need one to already exist
// before exercising login.
func registerAccount(t *testing.T, handler http.Handler, email string, password string) {
	t.Helper()
	response := postRegister(t, handler, url.Values{"email": {email}, "password": {password}})
	if response.Code != http.StatusFound {
		t.Fatalf("registering %q failed: status = %d, body: %s", email, response.Code, response.Body.String())
	}
}

func TestLoginWithCorrectCredentialsCreatesSessionAndRedirects(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)
	registerAccount(t, handler, "liam@example.com", "correct-password")

	response := postLogin(t, handler, url.Values{"email": {"Liam@Example.com"}, "password": {"correct-password"}})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	var found bool
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_session" && cookie.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("no session cookie set on successful login")
	}
}

// TestLoginFailuresProduceIdenticalGenericError covers the three ways a
// login attempt can fail — unknown email, wrong password, and an inactive
// account — all of which must produce the exact same generic error so that
// none of them leaks whether a given email is registered.
func TestLoginFailuresProduceIdenticalGenericError(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) (http.Handler, url.Values)
	}{
		{
			name: "unknown email",
			setup: func(t *testing.T) (http.Handler, url.Values) {
				handler, _ := buildRegisterTestHandler(t)
				registerAccount(t, handler, "maria@example.com", "correct-password")
				return handler, url.Values{"email": {"nobody@example.com"}, "password": {"whatever"}}
			},
		},
		{
			name: "wrong password",
			setup: func(t *testing.T) (http.Handler, url.Values) {
				handler, _ := buildRegisterTestHandler(t)
				registerAccount(t, handler, "maria@example.com", "correct-password")
				return handler, url.Values{"email": {"maria@example.com"}, "password": {"wrong-password"}}
			},
		},
		{
			name: "inactive account",
			setup: func(t *testing.T) (http.Handler, url.Values) {
				handler, sqlDB := buildLoginTestHandler(t)
				registerAccount(t, handler, "nora@example.com", "correct-password")
				if _, err := sqlDB.Exec("UPDATE account SET active = 0 WHERE email = ?", "nora@example.com"); err != nil {
					t.Fatalf("deactivate account: %v", err)
				}
				return handler, url.Values{"email": {"nora@example.com"}, "password": {"correct-password"}}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler, form := testCase.setup(t)

			response := postLogin(t, handler, form)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			// The rendered body naturally differs (the echoed email value,
			// each request's own CSRF token) — neither leaks whether an
			// email is registered. The actual enumeration-relevant signal
			// is the error message text itself, which must be identical
			// across all these failure modes.
			if !strings.Contains(response.Body.String(), "Invalid email or password.") {
				t.Fatalf("body does not contain the generic error: %s", response.Body.String())
			}
		})
	}
}

func TestLoginRedirectsToSafeNextAndIgnoresUnsafeNext(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)
	registerAccount(t, handler, "oscar@example.com", "correct-password")

	safe := postLogin(t, handler, url.Values{"email": {"oscar@example.com"}, "password": {"correct-password"}, "next": {"/dashboard/"}})
	if got := safe.Header().Get("Location"); got != "/dashboard/" {
		t.Fatalf("Location = %q, want %q", got, "/dashboard/")
	}

	unsafe := postLogin(t, handler, url.Values{"email": {"oscar@example.com"}, "password": {"correct-password"}, "next": {"https://evil.example.com/"}})
	if got := unsafe.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want %q (unsafe next ignored)", got, "/")
	}
}

func TestLoginMissingCSRFTokenIsRejected(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	request := httptest.NewRequest(http.MethodPost, "/accounts/login/", strings.NewReader("email=x@example.com&password=whatever"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestLoginIsRateLimitedAfterRepeatedFailures(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)
	registerAccount(t, handler, "penny@example.com", "correct-password")

	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = postLogin(t, handler, url.Values{"email": {"penny@example.com"}, "password": {"wrong-password"}})
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status after repeated failures = %d, want %d", last.Code, http.StatusTooManyRequests)
	}
}

func TestLoginPageHidesSignupLinkWhenDisabled(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t, accounts.WithSignupDisabled())

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/accounts/login/", nil))

	if strings.Contains(response.Body.String(), "/accounts/register/") {
		t.Fatal("login page links to registration even though signup is disabled")
	}
}

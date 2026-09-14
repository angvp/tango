package accounts_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
)

// buildProtectedTestHandler installs accounts alongside a host app
// contributing one route protected via accounts.RequireLogin, for testing
// the strict Active-gating mechanism this ticket adds. Returns the raw
// *sql.DB too, so a test can flip Active directly.
func buildProtectedTestHandler(t *testing.T) (http.Handler, *sql.DB) {
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

	protectedApp := tango.NewApp("dashboard", func(registry *tango.Registry) error {
		protected := accounts.RequireLogin(store, accounts.DefaultSessionCookieName, "/accounts/login/", func(ctx *tango.Context) error {
			return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
		})
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/dashboard/", protected),
		})
	})

	registry := tango.NewRegistry()
	if err := registry.Register(protectedApp); err != nil {
		t.Fatalf("register protected app: %v", err)
	}
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

func performProtectedRequest(t *testing.T, handler http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/dashboard/", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestActiveIsCheckedOnEveryRequestThroughAValidSession(t *testing.T) {
	handler, sqlDB := buildProtectedTestHandler(t)
	registerAccount(t, handler, "quinn@example.com", "correct-password")
	loginResponse := postLogin(t, handler, url.Values{"email": {"quinn@example.com"}, "password": {"correct-password"}})

	var sessionCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set a session cookie")
	}

	protectedResponse := performProtectedRequest(t, handler, sessionCookie)
	if protectedResponse.Code != http.StatusOK {
		t.Fatalf("status with a valid, active session = %d, want %d", protectedResponse.Code, http.StatusOK)
	}

	if _, err := sqlDB.Exec("UPDATE account SET active = 0 WHERE email = ?", "quinn@example.com"); err != nil {
		t.Fatalf("deactivate account: %v", err)
	}

	// The very next request through the SAME session must now be denied —
	// not wait for the session to expire.
	afterDeactivation := performProtectedRequest(t, handler, sessionCookie)
	if afterDeactivation.Code != http.StatusFound {
		t.Fatalf("status after deactivation = %d, want %d (redirect, same as unauthenticated)", afterDeactivation.Code, http.StatusFound)
	}
}

func TestRequireLoginRedirectsUnauthenticatedRequestToLoginWithNext(t *testing.T) {
	handler, _ := buildProtectedTestHandler(t)

	response := performProtectedRequest(t, handler, nil)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	location := response.Header().Get("Location")
	if location != "/accounts/login/?next=%2Fdashboard%2F" {
		t.Fatalf("Location = %q, want a redirect to login carrying next=/dashboard/", location)
	}
}

func TestRequireLoginRedirectsInvalidSessionCookieToLogin(t *testing.T) {
	handler, _ := buildProtectedTestHandler(t)

	response := performProtectedRequest(t, handler, &http.Cookie{Name: "tango_account_session", Value: "bogus-token"})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
}

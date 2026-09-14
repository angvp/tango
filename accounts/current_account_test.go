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

// buildWhoamiTestHandler installs accounts alongside a host route that
// calls accounts.CurrentAccount directly, mirroring how a real host app's
// own View would use the helper — not a redirecting guard like
// RequireLogin, just an identity lookup the View decides what to do with.
func buildWhoamiTestHandler(t *testing.T) http.Handler {
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

	hostApp := tango.NewApp("host", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/whoami/", func(ctx *tango.Context) error {
				account, ok, err := accounts.CurrentAccount(ctx, store, accounts.DefaultSessionCookieName)
				if err != nil {
					return err
				}
				if !ok {
					return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "no current account"})
				}
				return ctx.JSON(http.StatusOK, map[string]string{"email": account.Email})
			}),
		})
	})

	registry := tango.NewRegistry()
	if err := registry.Register(hostApp); err != nil {
		t.Fatalf("register host app: %v", err)
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
	return handler
}

func TestCurrentAccountReturnsIdentityForValidSession(t *testing.T) {
	handler := buildWhoamiTestHandler(t)
	registerAccount(t, handler, "tara@example.com", "correct-password")

	loginResponse := postLogin(t, handler, url.Values{"email": {"tara@example.com"}, "password": {"correct-password"}})
	var sessionCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set a session cookie")
	}

	request := httptest.NewRequest(http.MethodGet, "/whoami/", nil)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "tara@example.com") {
		t.Fatalf("body = %q, want it to contain the current account's email", response.Body.String())
	}
}

func TestCurrentAccountReportsNoAccountForMissingSession(t *testing.T) {
	handler := buildWhoamiTestHandler(t)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/whoami/", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

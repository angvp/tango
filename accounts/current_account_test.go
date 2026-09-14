package accounts_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
)

// buildWhoamiTestHandler installs accounts alongside host routes that call
// accounts.CurrentAccount/CurrentAccountID directly, mirroring how a real
// host app's own View would use the helpers — not a redirecting guard like
// RequireLogin, just an identity lookup the View decides what to do with.
// Returns the raw *sql.DB too, so a test can flip Active directly.
func buildWhoamiTestHandler(t *testing.T, opts ...accounts.Option) (http.Handler, *sql.DB) {
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
			tango.Path(http.MethodGet, "/whoami-id/", func(ctx *tango.Context) error {
				id, ok, err := accounts.CurrentAccountID(ctx, store, accounts.DefaultSessionCookieName)
				if err != nil {
					return err
				}
				if !ok {
					return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "no current account"})
				}
				return ctx.JSON(http.StatusOK, map[string]string{"id": fmt.Sprintf("%d", id)})
			}),
		})
	})

	registry := tango.NewRegistry()
	if err := registry.Register(hostApp); err != nil {
		t.Fatalf("register host app: %v", err)
	}
	if err := registry.Register(accounts.New(store, opts...)); err != nil {
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

func sessionCookieFrom(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			return cookie
		}
	}
	t.Fatal("response did not set a session cookie")
	return nil
}

func performWhoamiID(handler http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/whoami-id/", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCurrentAccountReturnsIdentityForValidSession(t *testing.T) {
	handler, _ := buildWhoamiTestHandler(t)
	registerAccount(t, handler, "tara@example.com", "correct-password")

	loginResponse := postLogin(t, handler, url.Values{"email": {"tara@example.com"}, "password": {"correct-password"}})
	sessionCookie := sessionCookieFrom(t, loginResponse)

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
	handler, _ := buildWhoamiTestHandler(t)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/whoami/", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestCurrentAccountIDReturnsIDForValidActiveSession(t *testing.T) {
	handler, _ := buildWhoamiTestHandler(t)
	registerAccount(t, handler, "uma@example.com", "correct-password")
	loginResponse := postLogin(t, handler, url.Values{"email": {"uma@example.com"}, "password": {"correct-password"}})
	sessionCookie := sessionCookieFrom(t, loginResponse)

	response := performWhoamiID(handler, sessionCookie)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"id":""`) {
		t.Fatalf("body = %q, want a non-empty id", response.Body.String())
	}
}

func TestCurrentAccountIDReportsNotOKForInvalidSessionCookie(t *testing.T) {
	handler, _ := buildWhoamiTestHandler(t)

	response := performWhoamiID(handler, &http.Cookie{Name: "tango_account_session", Value: "bogus-token"})

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestCurrentAccountIDReportsNotOKAfterAccountDeactivated(t *testing.T) {
	handler, sqlDB := buildWhoamiTestHandler(t)
	registerAccount(t, handler, "victor@example.com", "correct-password")
	loginResponse := postLogin(t, handler, url.Values{"email": {"victor@example.com"}, "password": {"correct-password"}})
	sessionCookie := sessionCookieFrom(t, loginResponse)

	if before := performWhoamiID(handler, sessionCookie); before.Code != http.StatusOK {
		t.Fatalf("status before deactivation = %d, want %d", before.Code, http.StatusOK)
	}

	if _, err := sqlDB.Exec("UPDATE account SET active = 0 WHERE email = ?", "victor@example.com"); err != nil {
		t.Fatalf("deactivate account: %v", err)
	}

	after := performWhoamiID(handler, sessionCookie)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("status after deactivation = %d, want %d — CurrentAccountID must use the same strict Active check as RequireLogin", after.Code, http.StatusUnauthorized)
	}
}

func TestCurrentAccountIDReportsNotOKForExpiredSession(t *testing.T) {
	// A negative session duration produces an AccountSession whose
	// ExpiresAt is already in the past the moment it's created — the
	// simplest way to exercise expiry without a fake clock or a sleep.
	handler, _ := buildWhoamiTestHandler(t, accounts.WithSessionDuration(-time.Hour))
	registerAccount(t, handler, "wendy@example.com", "correct-password")
	loginResponse := postLogin(t, handler, url.Values{"email": {"wendy@example.com"}, "password": {"correct-password"}})
	sessionCookie := sessionCookieFrom(t, loginResponse)

	response := performWhoamiID(handler, sessionCookie)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status for an already-expired session = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

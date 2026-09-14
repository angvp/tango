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

// TestCurrentAccountIdentityLookup covers accounts.CurrentAccount, which
// returns the full identity (here, the email surfaced in the JSON body)
// rather than just an ok/not-ok signal.
func TestCurrentAccountIdentityLookup(t *testing.T) {
	cases := []struct {
		name             string
		setup            func(t *testing.T) (http.Handler, *http.Cookie)
		wantStatus       int
		wantBodyContains string
	}{
		{
			name: "valid session returns the account's identity",
			setup: func(t *testing.T) (http.Handler, *http.Cookie) {
				handler, _ := buildWhoamiTestHandler(t)
				registerAccount(t, handler, "tara@example.com", "correct-password")
				loginResponse := postLogin(t, handler, url.Values{"email": {"tara@example.com"}, "password": {"correct-password"}})
				return handler, sessionCookieFrom(t, loginResponse)
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: "tara@example.com",
		},
		{
			name: "missing session reports no current account",
			setup: func(t *testing.T) (http.Handler, *http.Cookie) {
				handler, _ := buildWhoamiTestHandler(t)
				return handler, nil
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler, cookie := testCase.setup(t)

			request := httptest.NewRequest(http.MethodGet, "/whoami/", nil)
			if cookie != nil {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			if testCase.wantBodyContains != "" && !strings.Contains(response.Body.String(), testCase.wantBodyContains) {
				t.Fatalf("body = %q, want it to contain %q", response.Body.String(), testCase.wantBodyContains)
			}
		})
	}
}

// TestCurrentAccountIDLookup covers accounts.CurrentAccountID across the
// scenarios where it must report not-ok: an invalid cookie, a deactivated
// account, and an expired session, alongside the happy path.
func TestCurrentAccountIDLookup(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T) (http.Handler, *http.Cookie)
		wantStatus int
		checkBody  func(t *testing.T, body string)
	}{
		{
			name: "valid active session returns a non-empty id",
			setup: func(t *testing.T) (http.Handler, *http.Cookie) {
				handler, _ := buildWhoamiTestHandler(t)
				registerAccount(t, handler, "uma@example.com", "correct-password")
				loginResponse := postLogin(t, handler, url.Values{"email": {"uma@example.com"}, "password": {"correct-password"}})
				return handler, sessionCookieFrom(t, loginResponse)
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				if strings.Contains(body, `"id":""`) {
					t.Fatalf("body = %q, want a non-empty id", body)
				}
			},
		},
		{
			name: "invalid session cookie reports not ok",
			setup: func(t *testing.T) (http.Handler, *http.Cookie) {
				handler, _ := buildWhoamiTestHandler(t)
				return handler, &http.Cookie{Name: "tango_account_session", Value: "bogus-token"}
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "account deactivated after login reports not ok",
			setup: func(t *testing.T) (http.Handler, *http.Cookie) {
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
				return handler, sessionCookie
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "expired session reports not ok",
			setup: func(t *testing.T) (http.Handler, *http.Cookie) {
				// A negative session duration produces an AccountSession
				// whose ExpiresAt is already in the past the moment it's
				// created — the simplest way to exercise expiry without a
				// fake clock or a sleep.
				handler, _ := buildWhoamiTestHandler(t, accounts.WithSessionDuration(-time.Hour))
				registerAccount(t, handler, "wendy@example.com", "correct-password")
				loginResponse := postLogin(t, handler, url.Values{"email": {"wendy@example.com"}, "password": {"correct-password"}})
				return handler, sessionCookieFrom(t, loginResponse)
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler, cookie := testCase.setup(t)

			response := performWhoamiID(handler, cookie)

			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			if testCase.checkBody != nil {
				testCase.checkBody(t, response.Body.String())
			}
		})
	}
}

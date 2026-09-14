package admin_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

func buildLoginTestHandler(t *testing.T) (http.Handler, *db.Store) {
	t.Helper()

	registry := tango.NewRegistry()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE admin_user (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		active BOOLEAN NOT NULL,
		is_staff BOOLEAN NOT NULL,
		is_superuser BOOLEAN NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create admin_user: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE admin_session (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL,
		expires_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create admin_session: %v", err)
	}
	store := db.NewStore(sqlDB, db.SQLite)
	if err := admin.CreateAccount(context.Background(), store, "admin", "correct-password"); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}

	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, store
}

func doLoginRequest(t *testing.T, handler http.Handler, username string, password string) *httptest.ResponseRecorder {
	t.Helper()
	return postLogin(t, handler, url.Values{"username": {username}, "password": {password}})
}

// TestLoginRejectsInvalidCredentialsWithGenericError covers every account
// or credential state that must fail login with a generic 401: a wrong
// password, an unknown username, and a deactivated account presenting its
// correct password. All must produce the same generic error message (never
// leaking which part was wrong), and a wrong password must not set a
// session cookie.
func TestLoginRejectsInvalidCredentialsWithGenericError(t *testing.T) {
	tests := []struct {
		name string
		// setup optionally mutates account state before the login attempt
		// (e.g. deactivating it). It receives the store built alongside the
		// handler.
		setup            func(t *testing.T, store *db.Store)
		username         string
		password         string
		checkNoCookieSet bool
	}{
		{
			name:             "wrong password for a known username",
			username:         "admin",
			password:         "wrong-password",
			checkNoCookieSet: true,
		},
		{
			name:     "unknown username",
			username: "no-such-user",
			password: "whatever",
		},
		{
			name: "deactivated account with its correct password",
			setup: func(t *testing.T, store *db.Store) {
				if err := admin.Deactivate(context.Background(), store, "admin"); err != nil {
					t.Fatalf("deactivate: %v", err)
				}
			},
			username: "admin",
			password: "correct-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store := buildLoginTestHandler(t)
			if tt.setup != nil {
				tt.setup(t, store)
			}

			response := doLoginRequest(t, handler, tt.username, tt.password)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if !strings.Contains(response.Body.String(), "Invalid username or password") {
				t.Fatalf("body does not contain generic error message:\n%s", response.Body.String())
			}
			if tt.checkNoCookieSet && len(response.Result().Cookies()) != 0 {
				t.Fatal("a session cookie was set despite a failed login")
			}
		})
	}
}

func TestLoginWithCorrectCredentialsSetsSessionCookieAndRedirects(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	response := doLoginRequest(t, handler, "admin", "correct-password")

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", got)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "tango_admin_session" {
		t.Fatalf("cookies = %+v, want one tango_admin_session cookie", cookies)
	}
}

func TestLoginRedirectsToNextParameterOnSuccess(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	response := postLogin(t, handler, url.Values{"username": {"admin"}, "password": {"correct-password"}, "next": {"/admin/somewhere/"}})

	if got := response.Header().Get("Location"); got != "/admin/somewhere/" {
		t.Fatalf("Location = %q, want /admin/somewhere/", got)
	}
}

func TestLoginRejectsUnsafeNextParameterOnSuccess(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	for _, next := range []string{
		"https://evil.example/phish",
		"//evil.example/phish",
		"/elsewhere/",
		"/admin-evil/",
		"/admin",
	} {
		response := postLogin(t, handler, url.Values{"username": {"admin"}, "password": {"correct-password"}, "next": {next}})

		if got := response.Header().Get("Location"); got != "/admin/" {
			t.Fatalf("next %q redirected to %q, want /admin/", next, got)
		}
	}
}

func TestExpiredSessionIsTreatedAsUnauthenticated(t *testing.T) {
	handler, store := buildLoginTestHandler(t)

	response := doLoginRequest(t, handler, "admin", "correct-password")
	cookie := response.Result().Cookies()[0]

	// Force the session's expiry into the past directly, independent of
	// the fixed-duration constant HandleCLI/session.go uses.
	registry := model.NewRegistry()
	if err := registry.Register(admin.AdminSession{}); err != nil {
		t.Fatalf("register AdminSession: %v", err)
	}
	sessionMeta, _ := registry.Get("AdminSession")
	var sessions []admin.AdminSession
	if err := store.Query(context.Background(), &sessions, "SELECT id AS ID, token AS Token, user_id AS UserID, expires_at AS ExpiresAt FROM admin_session WHERE token = ?", cookie.Value); err != nil {
		t.Fatalf("query session: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	sessions[0].ExpiresAt = time.Now().Add(-time.Hour).UTC()
	if err := store.Update(context.Background(), sessionMeta, &sessions[0]); err != nil {
		t.Fatalf("update session: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect to login)", recorder.Code, http.StatusFound)
	}
	if got := recorder.Header().Get("Location"); !strings.HasPrefix(got, "/admin/login/") {
		t.Fatalf("Location = %q, want redirect to login", got)
	}
}

func TestLogoutDeletesSessionAndCookieNoLongerAuthenticates(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	loginResponse := doLoginRequest(t, handler, "admin", "correct-password")
	cookie := loginResponse.Result().Cookies()[0]

	logoutRequest := httptest.NewRequest(http.MethodPost, "/admin/logout/", nil)
	logoutRequest.AddCookie(cookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)

	if logoutResponse.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", logoutResponse.Code, http.StatusFound)
	}
	if got := logoutResponse.Header().Get("Location"); got != "/admin/login/" {
		t.Fatalf("logout Location = %q, want /admin/login/", got)
	}

	followUp := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	followUp.AddCookie(cookie)
	followUpResponse := httptest.NewRecorder()
	handler.ServeHTTP(followUpResponse, followUp)

	if followUpResponse.Code != http.StatusFound {
		t.Fatalf("status after logout = %d, want %d (redirect to login)", followUpResponse.Code, http.StatusFound)
	}
	if got := followUpResponse.Header().Get("Location"); !strings.HasPrefix(got, "/admin/login/") {
		t.Fatalf("Location after logout = %q, want redirect to login", got)
	}
}

func TestLoginIsRateLimitedAfterRepeatedFailures(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	// The rate limiter's threshold is an internal constant; 5 is safely
	// above what any reasonable threshold would allow, so this test isn't
	// coupled to its exact value beyond "eventually throttles."
	var last *httptest.ResponseRecorder
	for i := 0; i < 10; i++ {
		last = doLoginRequest(t, handler, "admin", "wrong-password")
		if last.Code == http.StatusTooManyRequests {
			break
		}
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status after repeated failures = %d, want %d eventually", last.Code, http.StatusTooManyRequests)
	}

	// Even the correct password is now throttled, per this milestone's
	// design (rate limiting by source, not by whether credentials would
	// have succeeded).
	blocked := doLoginRequest(t, handler, "admin", "correct-password")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("status for correct credentials while throttled = %d, want %d", blocked.Code, http.StatusTooManyRequests)
	}
}

func TestLogoutHasNoGetRoute(t *testing.T) {
	// /admin/logout/ is deliberately POST-only at the route level (not just
	// checked inside the view), so a bare link or prefetch can never log a
	// session out — see this milestone's destructive-action review. The
	// router itself (not logoutView) rejects GET, since only POST is
	// registered for this path.
	handler, _ := buildLoginTestHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/admin/logout/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d (no GET route)", response.Code, http.StatusMethodNotAllowed)
	}
}

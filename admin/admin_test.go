package admin_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	_ "modernc.org/sqlite"
)

type adminShellUser struct {
	ID    int64  `tango:"pk"`
	Email string `tango:"unique"`
}

type adminShellPost struct {
	ID    int64 `tango:"pk"`
	Title string
}

func TestAdminIndexRedirectsToFirstRegisteredModel(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/", cookie)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); got != "/admin/admin_shell_user/" {
		t.Fatalf("Location = %q, want %q", got, "/admin/admin_shell_user/")
	}
}

func TestAdminIndexWithoutTrailingSlashAlsoRedirects(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin", cookie)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); got != "/admin/admin_shell_user/" {
		t.Fatalf("Location = %q, want %q", got, "/admin/admin_shell_user/")
	}
}

func TestAdminIndexRequiresSession(t *testing.T) {
	handler, _ := buildAuthenticatedAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/", nil)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect to login)", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); !strings.HasPrefix(got, "/admin/login/") {
		t.Fatalf("Location = %q, want redirect to /admin/login/", got)
	}
}

func TestAdminMountsRoutesForRegisteredModel(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	for _, path := range []string{
		"/admin/admin_shell_user/",
		"/admin/admin_shell_user/new/",
		"/admin/admin_shell_user/42/",
		"/admin/admin_shell_user/42/delete/",
	} {
		response := performAdminRequest(handler, http.MethodGet, path, cookie)
		if response.Code == http.StatusNotFound {
			t.Fatalf("%s returned 404, want mounted admin route", path)
		}
		if response.Code == http.StatusFound {
			t.Fatalf("%s redirected (likely to login) with a valid session", path)
		}
	}
}

func TestAdminRequestWithoutSessionRedirectsToLoginWithNext(t *testing.T) {
	handler, _ := buildAuthenticatedAdminHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/admin/login/?next=") {
		t.Fatalf("Location = %q, want redirect to /admin/login/ with next", location)
	}
}

func TestAdminRequestWithInvalidSessionCookieRedirectsToLogin(t *testing.T) {
	handler, _ := buildAuthenticatedAdminHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: "bogus-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
}

func TestAdminRequestWithValidSessionReachesView(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/admin_shell_user/", cookie)

	if response.Code == http.StatusFound {
		t.Fatal("status = redirect, want request to reach view with a valid session")
	}
}

func TestAdminUnregisteredModelHasNoRoutes(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/admin_shell_post/", cookie)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

// buildAdminHandler builds a handler with one registered model
// (adminShellUser) and one seeded Admin account ("admin"/"secret"), but
// does not log in.
func buildAdminHandler(t *testing.T) http.Handler {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminShellUser{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminShellUser{}, admin.Options{
		ListDisplay: []string{"Email"},
		Search:      []string{"Email"},
		Ordering:    []string{"Email"},
	}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE admin_shell_user (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO admin_shell_user (id, email) VALUES (42, 'seed@example.com')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE admin_user (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		active BOOLEAN NOT NULL,
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
	if err := admin.CreateAccount(context.Background(), store, "admin", "secret"); err != nil {
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

	return handler
}

// buildAuthenticatedAdminHandler is buildAdminHandler plus a real login
// round-trip, returning the resulting session cookie for use in later
// requests.
func buildAuthenticatedAdminHandler(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	handler := buildAdminHandler(t)
	return handler, loginAndGetSessionCookie(t, handler, "admin", "secret")
}

func loginAndGetSessionCookie(t *testing.T, handler http.Handler, username string, password string) *http.Cookie {
	t.Helper()
	response := postLogin(t, handler, url.Values{"username": {username}, "password": {password}})

	if response.Code != http.StatusFound {
		t.Fatalf("login status = %d, want %d, body: %s", response.Code, http.StatusFound, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_admin_session" {
			return cookie
		}
	}
	t.Fatal("login response did not set a session cookie")
	return nil
}

func performAdminRequest(handler http.Handler, method string, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

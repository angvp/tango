package admin_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
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
	handler := buildAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/", true)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); got != "/admin/admin_shell_user/" {
		t.Fatalf("Location = %q, want %q", got, "/admin/admin_shell_user/")
	}
}

func TestAdminIndexWithoutTrailingSlashAlsoRedirects(t *testing.T) {
	handler := buildAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin", true)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); got != "/admin/admin_shell_user/" {
		t.Fatalf("Location = %q, want %q", got, "/admin/admin_shell_user/")
	}
}

func TestAdminIndexRequiresCredentials(t *testing.T) {
	handler := buildAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/", false)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAdminMountsRoutesForRegisteredModel(t *testing.T) {
	handler := buildAdminHandler(t)

	for _, path := range []string{
		"/admin/admin_shell_user/",
		"/admin/admin_shell_user/new/",
		"/admin/admin_shell_user/42/",
		"/admin/admin_shell_user/42/delete/",
	} {
		response := performAdminRequest(handler, http.MethodGet, path, true)
		if response.Code == http.StatusNotFound {
			t.Fatalf("%s returned 404, want mounted admin route", path)
		}
		if response.Code == http.StatusUnauthorized {
			t.Fatalf("%s returned 401 with correct credentials", path)
		}
	}
}

func TestAdminRequestWithoutCredentialsReturns401(t *testing.T) {
	handler := buildAdminHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if response.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("WWW-Authenticate header is empty")
	}
}

func TestAdminRequestWithWrongCredentialsReturns401(t *testing.T) {
	handler := buildAdminHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
	request.SetBasicAuth("admin", "wrong")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAdminRequestWithCorrectCredentialsReachesView(t *testing.T) {
	handler := buildAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/admin_shell_user/", true)

	if response.Code == http.StatusUnauthorized {
		t.Fatal("status = 401, want request to reach view")
	}
}

func TestAdminUnregisteredModelHasNoRoutes(t *testing.T) {
	handler := buildAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/admin_shell_post/", true)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

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
	store := db.NewStore(sqlDB, db.SQLite)

	if err := registry.Register(admin.New(store, admin.Credentials{
		Username: "admin",
		Password: "secret",
	})); err != nil {
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

func performAdminRequest(handler http.Handler, method string, path string, credentials bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if credentials {
		request.SetBasicAuth("admin", "secret")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

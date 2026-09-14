package admin_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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

// TestAdminSessionGating covers how requests to admin routes respond to
// every session/account state this milestone distinguishes: no session, an
// invalid session cookie, a valid session, an account deactivated after
// login, a non-staff account, and a staff (but non-superuser) account —
// plus the plain index-redirect behavior for an authenticated session.
func TestAdminSessionGating(t *testing.T) {
	tests := []struct {
		name string
		// build returns the handler and fully-formed request to send.
		build func(t *testing.T) (http.Handler, *http.Request)
		// check verifies the response for this scenario.
		check func(t *testing.T, response *httptest.ResponseRecorder)
	}{
		{
			name: "authenticated GET /admin/ redirects to the first registered model",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, cookie := buildAuthenticatedAdminHandler(t)
				request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
				request.AddCookie(cookie)
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusFound {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
				}
				if got := response.Header().Get("Location"); got != "/admin/admin_shell_user/" {
					t.Fatalf("Location = %q, want %q", got, "/admin/admin_shell_user/")
				}
			},
		},
		{
			name: "authenticated GET /admin (no trailing slash) also redirects to the first registered model",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, cookie := buildAuthenticatedAdminHandler(t)
				request := httptest.NewRequest(http.MethodGet, "/admin", nil)
				request.AddCookie(cookie)
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusFound {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
				}
				if got := response.Header().Get("Location"); got != "/admin/admin_shell_user/" {
					t.Fatalf("Location = %q, want %q", got, "/admin/admin_shell_user/")
				}
			},
		},
		{
			name: "GET /admin/ without a session redirects to login",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, _ := buildAuthenticatedAdminHandler(t)
				return handler, httptest.NewRequest(http.MethodGet, "/admin/", nil)
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusFound {
					t.Fatalf("status = %d, want %d (redirect to login)", response.Code, http.StatusFound)
				}
				if got := response.Header().Get("Location"); !strings.HasPrefix(got, "/admin/login/") {
					t.Fatalf("Location = %q, want redirect to /admin/login/", got)
				}
			},
		},
		{
			name: "GET on a model route without a session redirects to login with a next parameter",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, _ := buildAuthenticatedAdminHandler(t)
				return handler, httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusFound {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
				}
				location := response.Header().Get("Location")
				if !strings.HasPrefix(location, "/admin/login/?next=") {
					t.Fatalf("Location = %q, want redirect to /admin/login/ with next", location)
				}
			},
		},
		{
			name: "GET on a model route with an invalid session cookie redirects to login",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, _ := buildAuthenticatedAdminHandler(t)
				request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: "bogus-token"})
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusFound {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
				}
			},
		},
		{
			name: "GET on a model route with a valid session reaches the view",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, cookie := buildAuthenticatedAdminHandler(t)
				request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
				request.AddCookie(cookie)
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code == http.StatusFound {
					t.Fatal("status = redirect, want request to reach view with a valid session")
				}
			},
		},
		{
			name: "valid session for an account deactivated after login redirects to login, not 403",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, store, sqlDB := buildAdminHandlerWithStore(t)
				if err := admin.CreateAccount(context.Background(), store, "wasactive", "secret"); err != nil {
					t.Fatalf("seed admin account: %v", err)
				}
				cookie := loginAndGetSessionCookie(t, handler, "wasactive", "secret")

				// Flip Active directly (bypassing the CLI's Deactivate,
				// which would also invalidate the session) to isolate
				// exactly what an inactive-but-still-session-holding
				// account gets: a login redirect, the same as no session
				// at all — never 403, which is reserved for an
				// active-but-non-staff account. sessionUser's existing
				// !Active check runs before requireSession ever looks at
				// IsStaff, so this also confirms that ordering.
				if _, err := sqlDB.Exec("UPDATE admin_user SET active = 0 WHERE username = 'wasactive'"); err != nil {
					t.Fatalf("deactivate row directly: %v", err)
				}

				request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
				request.AddCookie(cookie)
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusFound {
					t.Fatalf("status = %d, want %d (redirect to login, not 403)", response.Code, http.StatusFound)
				}
				if got := response.Header().Get("Location"); !strings.HasPrefix(got, "/admin/login/") {
					t.Fatalf("Location = %q, want redirect to /admin/login/", got)
				}
			},
		},
		{
			name: "valid session for a non-staff account returns 403 Forbidden",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, store, _ := buildAdminHandlerWithStore(t)
				if err := admin.CreateAccount(context.Background(), store, "guest", "secret", admin.WithoutStaff()); err != nil {
					t.Fatalf("seed non-staff admin account: %v", err)
				}
				cookie := loginAndGetSessionCookie(t, handler, "guest", "secret")

				request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
				request.AddCookie(cookie)
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want %d (403 Forbidden)", response.Code, http.StatusForbidden)
				}
			},
		},
		{
			name: "valid session for a staff-only account (not superuser) reaches the view",
			build: func(t *testing.T) (http.Handler, *http.Request) {
				handler, store, _ := buildAdminHandlerWithStore(t)
				if err := admin.CreateAccount(context.Background(), store, "staffonly", "secret", admin.WithoutSuperuser()); err != nil {
					t.Fatalf("seed staff-only admin account: %v", err)
				}
				cookie := loginAndGetSessionCookie(t, handler, "staffonly", "secret")

				request := httptest.NewRequest(http.MethodGet, "/admin/admin_shell_user/", nil)
				request.AddCookie(cookie)
				return handler, request
			},
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				if response.Code == http.StatusForbidden {
					t.Fatal("status = 403, want a staff account (regardless of IsSuperuser) to reach the view")
				}
				if response.Code == http.StatusFound {
					t.Fatal("status = redirect, want request to reach view with a valid staff session")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, request := tt.build(t)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			tt.check(t, response)
		})
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

func TestAdminUnregisteredModelHasNoRoutes(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	response := performAdminRequest(handler, http.MethodGet, "/admin/admin_shell_post/", cookie)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestAdminWithMiddlewareWrapsOnlyAdminRoutesAfterGlobalMiddleware(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := db.NewStore(sqlDB, db.SQLite)
	publicApp := tango.NewApp("public", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/public/", func(ctx *tango.Context) error {
				ctx.ResponseWriter().Header().Add("X-Middleware-Order", "view")
				return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
			}),
		})
	})
	config := tango.Config{
		InstalledApps: []tango.App{
			publicApp,
			admin.New(store, admin.WithMiddleware(headerOrderMiddleware("admin"))),
		},
		Middleware: []tango.Middleware{headerOrderMiddleware("global")},
	}

	registry, err := tango.BuildRegistry(config)
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	adminResponse := httptest.NewRecorder()
	handler.ServeHTTP(adminResponse, httptest.NewRequest(http.MethodGet, "/admin/login/", nil))
	if got, want := adminResponse.Header().Values("X-Middleware-Order"), []string{"global", "admin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("admin middleware order = %v, want %v", got, want)
	}

	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, httptest.NewRequest(http.MethodGet, "/public/", nil))
	if got, want := publicResponse.Header().Values("X-Middleware-Order"), []string{"global", "view"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("public middleware order = %v, want %v", got, want)
	}
}

// buildAdminHandler builds a handler with one registered model
// (adminShellUser) and one seeded Admin account ("admin"/"secret"), but
// does not log in.
func buildAdminHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, _, _ := buildAdminHandlerWithStore(t)
	return handler
}

// buildAdminHandlerWithStore is buildAdminHandler plus the underlying store
// and raw *sql.DB, for tests that need to seed additional accounts (e.g. a
// non-staff one) or reach state Store's typed API doesn't expose (e.g.
// flipping Active directly, bypassing Deactivate's session invalidation).
func buildAdminHandlerWithStore(t *testing.T) (http.Handler, *db.Store, *sql.DB) {
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

	return handler, store, sqlDB
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

func headerOrderMiddleware(marker string) tango.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Middleware-Order", marker)
			next.ServeHTTP(w, r)
		})
	}
}

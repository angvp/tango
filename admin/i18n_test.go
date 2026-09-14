package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	_ "modernc.org/sqlite"
)

const adminI18NLocale = "zz-admin-i18n"

func adminI18NMiddleware() tango.Middleware {
	return i18n.Middleware(func(*http.Request) string { return adminI18NLocale })
}

func TestAdminLoginFallsBackWithoutI18nMiddleware(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.login.username": "Usuario",
	})

	response := performAdminRequest(handler, http.MethodGet, "/admin/login/", nil)

	if !strings.Contains(response.Body.String(), "Username") {
		t.Fatalf("login did not render English fallback without middleware:\n%s", response.Body.String())
	}
}

func TestAdminLoginTranslatesWithI18nMiddleware(t *testing.T) {
	handler, _ := buildLoginI18NTestHandler(t)
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.login.title":      "Administración tanGO",
		"admin.login.username":   "Usuario",
		"admin.login.password":   "Contraseña",
		"admin.login.submit":     "Entrar",
		"admin.layout.open_menu": "Abrir menú",
	})

	response := performAdminRequest(handler, http.MethodGet, "/admin/login/", nil)
	body := response.Body.String()

	for _, want := range []string{"Administración tanGO", "Usuario", "Contraseña", "Entrar"} {
		if !strings.Contains(body, want) {
			t.Fatalf("login body missing translated text %q:\n%s", want, body)
		}
	}
}

func TestAdminLoginFallsBackForUnregisteredLocale(t *testing.T) {
	handler, _ := buildLoginTestHandlerWithMiddleware(t, i18n.Middleware(func(*http.Request) string { return "zz-missing-locale" }))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.login.username": "Usuario",
	})

	response := performAdminRequest(handler, http.MethodGet, "/admin/login/", nil)

	if !strings.Contains(response.Body.String(), "Username") {
		t.Fatalf("login did not render English fallback for unregistered locale:\n%s", response.Body.String())
	}
}

func TestAdminCRUDSurfacesTranslateWithI18nMiddleware(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Search:      []string{"Name"},
		Ordering:    []string{"Name"},
	}, admin.WithMiddleware(adminI18NMiddleware()))
	id := seedProduct(t, sqlDB, "Widget", 9.99)
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.button.add_model":        "+ Crear %s",
		"admin.button.edit":             "Editar",
		"admin.button.delete":           "Borrar",
		"admin.button.search":           "Buscar",
		"admin.button.save":             "Guardar",
		"admin.button.cancel":           "Cancelar",
		"admin.button.confirm_delete":   "Confirmar borrado",
		"admin.create.title":            "Nuevo %s",
		"admin.edit.title":              "Editar %s",
		"admin.delete.title":            "Borrar %s",
		"admin.delete.confirm_prefix":   "Seguro que quieres borrar",
		"admin.delete.confirm_suffix":   "Esta acción no se puede deshacer.",
		"admin.layout.models":           "Modelos",
		"admin.layout.signed_in":        "Sesión iniciada",
		"admin.list.search_placeholder": "Buscar %s…",
		"admin.pagination.label":        "Paginación",
		"admin.pagination.previous":     "Página anterior",
		"admin.pagination.next":         "Página siguiente",
	})

	list := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(list, "Crear crudProduct") || !strings.Contains(list, "Buscar") || !strings.Contains(list, "Modelos") || !strings.Contains(list, "Editar") {
		t.Fatalf("list body missing translated text:\n%s", list)
	}

	create := doRequest(t, handler, http.MethodGet, crudBasePath+"new/", nil).Body.String()
	if !strings.Contains(create, "Nuevo crudProduct") || !strings.Contains(create, "Guardar") {
		t.Fatalf("create body missing translated text:\n%s", create)
	}

	edit := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/", crudBasePath, id), nil).Body.String()
	if !strings.Contains(edit, "Editar crudProduct") || !strings.Contains(edit, "Cancelar") {
		t.Fatalf("edit body missing translated text:\n%s", edit)
	}

	del := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil).Body.String()
	if !strings.Contains(del, "Borrar crudProduct") || !strings.Contains(del, "Confirmar borrado") {
		t.Fatalf("delete body missing translated text:\n%s", del)
	}
}

func TestAdminErrorsTranslateWithI18nMiddleware(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
	}, admin.WithMiddleware(adminI18NMiddleware()))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.error.csrf": "token CSRF inválido",
	})

	csrfForm := url.Values{"Name": {"Widget"}, "Price": {"9.99"}}
	csrfRequest := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(csrfForm.Encode()))
	csrfRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrfRequest.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	if !strings.Contains(csrfResponse.Body.String(), "token CSRF inválido") {
		t.Fatalf("CSRF response missing translated error:\n%s", csrfResponse.Body.String())
	}

}

func TestAdminRateLimitTranslatesWithI18nMiddleware(t *testing.T) {
	handler, _ := buildLoginI18NTestHandler(t)
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.error.too_many_login_attempts": "demasiados intentos",
	})

	var response *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		response = postLogin(t, handler, url.Values{"username": {"admin"}, "password": {"wrong-password"}})
	}

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
	if !strings.Contains(response.Body.String(), "demasiados intentos") {
		t.Fatalf("rate limit response missing translated error:\n%s", response.Body.String())
	}
}

func buildLoginI18NTestHandler(t *testing.T) (http.Handler, *db.Store) {
	t.Helper()
	return buildLoginTestHandlerWithMiddleware(t, adminI18NMiddleware())
}

func buildLoginTestHandlerWithMiddleware(t *testing.T, middleware tango.Middleware) (http.Handler, *db.Store) {
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

	if err := registry.Register(admin.New(store, admin.WithMiddleware(middleware))); err != nil {
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

func TestAdminForbiddenTranslatesWithI18nMiddleware(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
	}, admin.WithMiddleware(adminI18NMiddleware()))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.error.not_staff": "sin acceso staff",
	})
	if _, err := sqlDB.Exec("UPDATE admin_user SET is_staff = 0 WHERE username = 'admin'"); err != nil {
		t.Fatalf("make admin non-staff: %v", err)
	}
	if _, err := sqlDB.Exec("UPDATE admin_session SET expires_at = ? WHERE token = ?", time.Now().Add(time.Hour).UTC(), testSessionToken); err != nil {
		t.Fatalf("refresh session: %v", err)
	}

	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !strings.Contains(response.Body.String(), "sin acceso staff") {
		t.Fatalf("forbidden response missing translated error:\n%s", response.Body.String())
	}
}

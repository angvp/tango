package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/internal/adminregistry"
)

// pagesHandler builds an admin with the given pages registered before it is
// installed, one model, and two accounts: "admin" (staff) and "guest" (not).
func pagesHandler(t *testing.T, pages ...admin.Page) http.Handler {
	t.Helper()
	handler, _ := pagesHandlerWith(t, nil, pages...)
	return handler
}

// pagesHandlerWith is pagesHandler with admin options, and the raw database
// for tests that need to age or deactivate a session directly.
func pagesHandlerWith(t *testing.T, options []admin.Option, pages ...admin.Page) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminShellUser{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminShellUser{}, admin.Options{ListDisplay: []string{"Email"}}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	for _, page := range pages {
		if err := admin.RegisterPage(registry, page); err != nil {
			t.Fatalf("RegisterPage(%q): %v", page.Path, err)
		}
	}
	sqlDB, store := migratedAdminDB(t, registry)
	ctx := context.Background()
	if err := admin.CreateAccount(ctx, store, "admin", "secret"); err != nil {
		t.Fatalf("seed staff account: %v", err)
	}
	if err := admin.CreateAccount(ctx, store, "guest", "secret", admin.WithoutStaff()); err != nil {
		t.Fatalf("seed non-staff account: %v", err)
	}
	if err := registry.Register(admin.New(store, options...)); err != nil {
		t.Fatalf("register admin app: %v", err)
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

func textPage(path, title, body string, nav bool) admin.Page {
	return admin.Page{Path: path, Title: title, Nav: nav, View: func(ctx *tango.Context) error {
		ctx.ResponseWriter().Header().Set("Content-Type", "text/html; charset=utf-8")
		ctx.ResponseWriter().WriteHeader(http.StatusOK)
		_, err := ctx.ResponseWriter().Write([]byte(body))
		return err
	}}
}

func TestAdminPageIsServedToStaffUnderAdmin(t *testing.T) {
	handler := pagesHandler(t, textPage("logos", "Client logos", "<p>logo page body</p>", false))
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")

	response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "logo page body") {
		t.Fatalf("body does not hold the page's output: %s", response.Body.String())
	}
}

func TestAdminPageGate(t *testing.T) {
	handler := pagesHandler(t, textPage("logos", "Client logos", "<p>logo page body</p>", false))

	t.Run("a visitor who is not signed in goes to the login and comes back", func(t *testing.T) {
		response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", nil)
		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", response.Code)
		}
		if got, want := response.Header().Get("Location"), "/admin/login/?next=%2Fadmin%2Flogos%2F"; got != want {
			t.Fatalf("Location = %q, want %q", got, want)
		}
	})

	t.Run("signing in with next lands on the page", func(t *testing.T) {
		response := postLogin(t, handler, url.Values{"username": {"admin"}, "password": {"secret"}, "next": {"/admin/logos/"}})
		if got := response.Header().Get("Location"); got != "/admin/logos/" {
			t.Fatalf("login redirected to %q, want /admin/logos/", got)
		}
	})

	t.Run("a signed-in account without staff access gets the 403 page", func(t *testing.T) {
		cookie := loginAndGetSessionCookie(t, handler, "guest", "secret")
		response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)
		if response.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", response.Code)
		}
		if strings.Contains(response.Body.String(), "logo page body") {
			t.Fatal("a non-staff account saw the page's output")
		}
	})

	t.Run("an anonymous POST is sent to the login like a GET", func(t *testing.T) {
		if got := performAdminRequest(handler, http.MethodPost, "/admin/logos/", nil).Code; got != http.StatusFound {
			t.Fatalf("anonymous POST status = %d, want 302 to the login", got)
		}
	})

	t.Run("another method gets the admin's method-not-allowed answer", func(t *testing.T) {
		cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
		if got := performAdminRequest(handler, http.MethodDelete, "/admin/logos/", cookie).Code; got != http.StatusMethodNotAllowed {
			t.Fatalf("DELETE status = %d, want 405", got)
		}
	})
}

func TestAdminPageNavigation(t *testing.T) {
	handler := pagesHandler(t,
		textPage("logos", "Client logos", "<p>one</p>", true),
		textPage("hidden-tool", "Hidden tool", "<p>two</p>", false),
	)
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")

	body := performAdminRequest(handler, http.MethodGet, "/admin/admin_shell_user/", cookie).Body.String()

	if !strings.Contains(body, `href="/admin/logos/"`) || !strings.Contains(body, "Client logos") {
		t.Fatalf("a Nav page is missing from the sidebar: %s", body)
	}
	if strings.Contains(body, "/admin/hidden-tool/") || strings.Contains(body, "Hidden tool") {
		t.Fatal("a page with Nav false is listed in the sidebar")
	}
	if strings.Index(body, `href="/admin/logos/"`) < strings.Index(body, `href="/admin/admin_shell_user/"`) {
		t.Fatal("the page is listed before the models, want it after")
	}
}

func TestRegisterPageRefusals(t *testing.T) {
	view := func(*tango.Context) error { return nil }
	tests := []struct {
		name string
		page admin.Page
		want error
	}{
		{"empty path", admin.Page{View: view}, admin.ErrInvalidPage},
		{"uppercase path", admin.Page{Path: "Logos", View: view}, admin.ErrInvalidPage},
		{"nested path", admin.Page{Path: "a/b", View: view}, admin.ErrInvalidPage},
		{"leading underscore", admin.Page{Path: "_assets", View: view}, admin.ErrInvalidPage},
		{"no view", admin.Page{Path: "logos"}, admin.ErrInvalidPage},
		{"nav without a title", admin.Page{Path: "logos", Nav: true, View: view}, admin.ErrInvalidPage},
		{"login is reserved", admin.Page{Path: "login", View: view}, admin.ErrReservedPagePath},
		{"logout is reserved", admin.Page{Path: "logout", View: view}, admin.ErrReservedPagePath},
		{"theme is reserved", admin.Page{Path: "theme", View: view}, admin.ErrReservedPagePath},
		{"messages is reserved", admin.Page{Path: "messages", View: view}, admin.ErrReservedPagePath},
		{"density is reserved", admin.Page{Path: "density", View: view}, admin.ErrReservedPagePath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := admin.RegisterPage(tango.NewRegistry(), tc.page)
			if !errors.Is(err, tc.want) {
				t.Fatalf("RegisterPage = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("a second page with the same path", func(t *testing.T) {
		registry := tango.NewRegistry()
		if err := admin.RegisterPage(registry, admin.Page{Path: "logos", View: view}); err != nil {
			t.Fatal(err)
		}
		if err := admin.RegisterPage(registry, admin.Page{Path: "logos", View: view}); !errors.Is(err, admin.ErrDuplicatePage) {
			t.Fatalf("second RegisterPage = %v, want ErrDuplicatePage", err)
		}
	})

	t.Run("a nil registry", func(t *testing.T) {
		if err := admin.RegisterPage(nil, admin.Page{Path: "logos", View: view}); !errors.Is(err, admin.ErrInvalidPage) {
			t.Fatalf("RegisterPage(nil) = %v, want ErrInvalidPage", err)
		}
	})
}

func TestAdminRefusesAPageThatSharesAModelPath(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminShellUser{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminShellUser{}, admin.Options{ListDisplay: []string{"Email"}}); err != nil {
		t.Fatal(err)
	}
	if err := admin.RegisterPage(registry, textPage("admin_shell_user", "Clash", "x", false)); err != nil {
		t.Fatalf("RegisterPage: %v", err)
	}
	_, store := migratedAdminDB(t, registry)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}

	err := registry.RunRegistration()

	if !errors.Is(err, admin.ErrInvalidPage) || !strings.Contains(err.Error(), "adminShellUser") {
		t.Fatalf("RunRegistration = %v, want ErrInvalidPage naming the model", err)
	}
}

func TestAdminChecksReportAPageRegisteredAfterInstall(t *testing.T) {
	registry := tango.NewRegistry()
	_, store := migratedAdminDB(t, registry)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	checkFailures := func() []string {
		var failures []string
		for _, check := range registry.Checks() {
			if check.Err != nil {
				failures = append(failures, check.Err.Error())
			}
		}
		return failures
	}
	if got := checkFailures(); len(got) != 0 {
		t.Fatalf("checks before the late page = %v, want none", got)
	}

	if err := admin.RegisterPage(registry, textPage("late-tool", "Late", "x", false)); err != nil {
		t.Fatalf("RegisterPage: %v", err)
	}

	got := checkFailures()
	if len(got) != 1 || !strings.Contains(got[0], "late-tool") {
		t.Fatalf("checks = %v, want one failure naming the page", got)
	}
}

func TestAdminPageViewReadsTheAccountAndRendersInTheLayout(t *testing.T) {
	var user, token string
	var signedIn bool
	page := admin.Page{Path: "logos", Title: "Client logos", Nav: true, View: func(ctx *tango.Context) error {
		user, signedIn = admin.CurrentUser(ctx)
		token = admin.CSRFToken(ctx)
		return admin.RenderPage(ctx, "Upload a logo", template.HTML(`<form method="post"><input type="hidden" name="csrf_token" value="`+token+`"></form>`))
	}}
	handler := pagesHandler(t, page)
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")

	response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	if !signedIn || user != "admin" {
		t.Fatalf("CurrentUser = %q, %v, want admin, true", user, signedIn)
	}
	if token == "" {
		t.Fatal("CSRFToken is empty inside a page")
	}
	body := response.Body.String()
	for _, want := range []string{"Upload a logo", `value="` + token + `"`, `href="/admin/admin_shell_user/"`, "<title>Upload a logo"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	if !strings.Contains(body, `aria-current="page"`) {
		t.Error("the sidebar does not mark the current page")
	}
}

func TestAdminCurrentUserIsEmptyOutsideTheAdminGate(t *testing.T) {
	registry := tango.NewRegistry()
	var user, token string
	var signedIn bool
	err := registry.Routes().Include("/", tango.URLs{tango.Path(http.MethodGet, "/open/", func(ctx *tango.Context) error {
		user, signedIn = admin.CurrentUser(ctx)
		token = admin.CSRFToken(ctx)
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/open/", nil))

	if signedIn || user != "" || token != "" {
		t.Fatalf("outside the gate CurrentUser = %q, %v and CSRFToken = %q, want zero values", user, signedIn, token)
	}
}

func TestAdminPagePostNeedsTheAdminCSRFToken(t *testing.T) {
	var runs int
	var token string
	page := admin.Page{Path: "logos", View: func(ctx *tango.Context) error {
		token = admin.CSRFToken(ctx)
		if ctx.Request().Method == http.MethodPost {
			runs++
		}
		return admin.RenderPage(ctx, "Logos", "<p>ok</p>")
	}}
	handler := pagesHandler(t, page)
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
	performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)
	valid := token

	post := func(form url.Values) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/admin/logos/", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	for name, form := range map[string]url.Values{
		"no token":    {},
		"wrong token": {"csrf_token": {"not-the-token"}},
	} {
		if response := post(form); response.Code != http.StatusForbidden || runs != 0 {
			t.Errorf("%s: status = %d, view ran %d times, want 403 and 0", name, response.Code, runs)
		}
	}
	if response := post(url.Values{"csrf_token": {valid}}); response.Code != http.StatusOK || runs != 1 {
		t.Fatalf("valid token: status = %d, view ran %d times, want 200 and 1", response.Code, runs)
	}
}

// A reusable app contributes a page from its own registration function, as it
// contributes a model, and a host that installs it before the admin serves it.
func TestAReusableAppContributesAnAdminPage(t *testing.T) {
	registry := tango.NewRegistry()
	greetings := tango.NewApp("greetings", func(registry *tango.Registry) error {
		return admin.RegisterPage(registry, textPage("greetings", "Greetings", "<p>from the app</p>", true))
	})
	_, store := migratedAdminDB(t, registry)
	if err := admin.CreateAccount(context.Background(), store, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(greetings); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")

	response := performAdminRequest(handler, http.MethodGet, "/admin/greetings/", cookie)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "from the app") {
		t.Fatalf("status = %d, body = %q, want the app's page", response.Code, response.Body.String())
	}
}

func TestAdminPageRedirectsAnExpiredOrDeactivatedSession(t *testing.T) {
	page := textPage("logos", "Client logos", "<p>logo page body</p>", false)

	t.Run("an expired session", func(t *testing.T) {
		handler, sqlDB := pagesHandlerWith(t, nil, page)
		cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
		if _, err := sqlDB.Exec("UPDATE admin_session SET expires_at = $1", time.Now().Add(-time.Hour).UTC()); err != nil {
			t.Fatalf("expire the session: %v", err)
		}
		response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)
		if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "/admin/login/?next=") {
			t.Fatalf("status = %d Location = %q, want a redirect to the login", response.Code, response.Header().Get("Location"))
		}
	})

	t.Run("an account deactivated after login", func(t *testing.T) {
		handler, sqlDB := pagesHandlerWith(t, nil, page)
		cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
		if _, err := sqlDB.Exec("UPDATE admin_user SET active = FALSE WHERE username = 'admin'"); err != nil {
			t.Fatalf("deactivate the account: %v", err)
		}
		response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)
		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want a redirect to the login, not 403", response.Code)
		}
	})
}

func TestAdminPageMultipartPostIsCheckedForTheCSRFToken(t *testing.T) {
	var runs int
	var token string
	page := admin.Page{Path: "logos", View: func(ctx *tango.Context) error {
		token = admin.CSRFToken(ctx)
		if ctx.Request().Method == http.MethodPost {
			runs++
		}
		return admin.RenderPage(ctx, "Logos", "<p>ok</p>")
	}}
	handler := pagesHandler(t, page)
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
	performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)

	post := func(csrf string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if csrf != "" {
			_ = writer.WriteField("csrf_token", csrf)
		}
		file, _ := writer.CreateFormFile("logo", "logo.png")
		_, _ = file.Write([]byte("not really a png"))
		_ = writer.Close()
		request := httptest.NewRequest(http.MethodPost, "/admin/logos/", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if response := post(""); response.Code != http.StatusForbidden || runs != 0 {
		t.Fatalf("multipart without a token: status = %d, view ran %d times, want 403 and 0", response.Code, runs)
	}
	if response := post("wrong"); response.Code != http.StatusForbidden || runs != 0 {
		t.Fatalf("multipart with a wrong token: status = %d, view ran %d times, want 403 and 0", response.Code, runs)
	}
	if response := post(token); response.Code != http.StatusOK || runs != 1 {
		t.Fatalf("multipart with the token: status = %d, view ran %d times, want 200 and 1", response.Code, runs)
	}
}

func TestAdminRefusesAPageWhoseViewIsNotATangoView(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Admin().AddPage(adminregistryEntry("logos", "not a view")); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	_, store := migratedAdminDB(t, registry)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}

	if err := registry.RunRegistration(); !errors.Is(err, admin.ErrInvalidPage) {
		t.Fatalf("RunRegistration = %v, want ErrInvalidPage instead of a panic at request time", err)
	}
}

func TestAdminPageRendersUnderTheComposedSecurityPolicy(t *testing.T) {
	page := admin.Page{Path: "logos", Title: "Logos", Nav: true, View: func(ctx *tango.Context) error {
		return admin.RenderPage(ctx, "Logos", "<p>inside the layout</p>")
	}}
	handler, _ := pagesHandlerWith(t, composedAdminOptions(), page)
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")

	response := performAdminRequest(handler, http.MethodGet, "/admin/logos/", cookie)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if values := response.Header().Values("Content-Security-Policy"); len(values) != 1 || values[0] != adminPolicyHeader {
		t.Fatalf("Content-Security-Policy = %q, want exactly [%q]", values, adminPolicyHeader)
	}
	body := response.Body.String()
	for _, banned := range []string{"style=", "onclick=", "<script>"} {
		if strings.Contains(body, banned) {
			t.Errorf("the page holds %q, which the strict policy blocks", banned)
		}
	}
}

func adminregistryEntry(path string, view any) adminregistry.PageEntry {
	return adminregistry.PageEntry{Path: path, View: view}
}

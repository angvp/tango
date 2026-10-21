package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const themeBasePath = "/admin/theme/"

// postTheme sends a theme POST carrying the session cookie and the session's
// CSRF token, plus any extra form values.
func postTheme(t *testing.T, handler http.Handler, form url.Values, extra ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	submitted := url.Values{"csrf_token": {testSessionCSRFToken}}
	for key, values := range form {
		submitted[key] = values
	}
	request := httptest.NewRequest(http.MethodPost, themeBasePath, strings.NewReader(submitted.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	for _, cookie := range extra {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func themeCookieFrom(response *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_admin_theme" {
			return cookie
		}
	}
	return nil
}

func getWithCookies(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestThemeCookieIsSetForLightAndDarkWithCSRF(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		handler, _ := buildProductAdmin(t)
		response := postTheme(t, handler, url.Values{"theme": {theme}, "next": {"/admin/crud_product/"}})

		if response.Code != http.StatusFound {
			t.Fatalf("%s: status = %d, want 302", theme, response.Code)
		}
		cookie := themeCookieFrom(response)
		if cookie == nil {
			t.Fatalf("%s: no tango_admin_theme cookie set", theme)
		}
		if cookie.Value != theme {
			t.Errorf("%s: cookie value = %q", theme, cookie.Value)
		}
		if cookie.Path != "/admin/" {
			t.Errorf("%s: cookie path = %q, want /admin/", theme, cookie.Path)
		}
		if !cookie.HttpOnly {
			t.Errorf("%s: cookie is not HttpOnly", theme)
		}
		if cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s: SameSite = %v, want Lax", theme, cookie.SameSite)
		}
		if cookie.MaxAge != 365*24*60*60 {
			t.Errorf("%s: MaxAge = %d, want one year", theme, cookie.MaxAge)
		}
		if cookie.Secure {
			t.Errorf("%s: cookie is Secure over plain HTTP", theme)
		}
		if got := response.Header().Get("Location"); got != "/admin/crud_product/" {
			t.Errorf("%s: Location = %q", theme, got)
		}
	}
}

func TestThemeCookieSecureFollowsForwardedProto(t *testing.T) {
	for _, tt := range []struct {
		proto string
		want  bool
	}{{"https", true}, {"", false}} {
		handler, _ := buildProductAdmin(t)
		request := httptest.NewRequest(http.MethodPost, themeBasePath, strings.NewReader(url.Values{"csrf_token": {testSessionCSRFToken}, "theme": {"dark"}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
		if tt.proto != "" {
			request.Header.Set("X-Forwarded-Proto", tt.proto)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		cookie := themeCookieFrom(response)
		if cookie == nil || cookie.Secure != tt.want {
			t.Errorf("X-Forwarded-Proto %q: cookie = %+v, want Secure %v", tt.proto, cookie, tt.want)
		}
	}
}

func TestSystemDeletesTheThemeCookie(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	response := postTheme(t, handler, url.Values{"theme": {"system"}},
		&http.Cookie{Name: "tango_admin_theme", Value: "dark"})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	cookie := themeCookieFrom(response)
	if cookie == nil {
		t.Fatal("system did not send a Set-Cookie clearing tango_admin_theme")
	}
	if cookie.MaxAge >= 0 || cookie.Value != "" {
		t.Errorf("cleared cookie = %+v, want MaxAge < 0 and empty value", cookie)
	}
	if cookie.Path != "/admin/" || !cookie.HttpOnly {
		t.Errorf("cleared cookie attributes = %+v, want Path /admin/ and HttpOnly", cookie)
	}
}

func TestThemeRefusesValuesOtherThanLightDarkSystem(t *testing.T) {
	for _, theme := range []string{"", "Dark", "DARK", "auto", "system ", "dark;path=/", "<script>"} {
		handler, _ := buildProductAdmin(t)
		response := postTheme(t, handler, url.Values{"theme": {theme}})

		if response.Code != http.StatusBadRequest {
			t.Errorf("theme %q: status = %d, want 400", theme, response.Code)
		}
		if cookie := themeCookieFrom(response); cookie != nil {
			t.Errorf("theme %q: cookie set or cleared: %+v", theme, cookie)
		}
	}
}

func TestThemeIsNotSetWithoutCSRFOrSession(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	request := httptest.NewRequest(http.MethodPost, themeBasePath, strings.NewReader(url.Values{"theme": {"dark"}, "csrf_token": {"wrong"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Errorf("wrong CSRF: status = %d, want 403", response.Code)
	}
	if cookie := themeCookieFrom(response); cookie != nil {
		t.Errorf("wrong CSRF set a theme cookie: %+v", cookie)
	}

	anonymous := httptest.NewRequest(http.MethodPost, themeBasePath, strings.NewReader(url.Values{"theme": {"dark"}, "csrf_token": {testSessionCSRFToken}}.Encode()))
	anonymous.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	anonymousResponse := httptest.NewRecorder()
	handler.ServeHTTP(anonymousResponse, anonymous)
	if anonymousResponse.Code != http.StatusFound || anonymousResponse.Header().Get("Location") != "/admin/login/?next=%2Fadmin%2Ftheme%2F" {
		t.Errorf("no session: status = %d, Location = %q, want redirect to login", anonymousResponse.Code, anonymousResponse.Header().Get("Location"))
	}
	if cookie := themeCookieFrom(anonymousResponse); cookie != nil {
		t.Errorf("no session set a theme cookie: %+v", cookie)
	}
}

func TestThemeIsRefusedForNonStaffAccount(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	if _, err := sqlDB.Exec(`UPDATE admin_user SET is_staff = $1 WHERE username = $2`, false, "admin"); err != nil {
		t.Fatalf("demote account: %v", err)
	}

	response := postTheme(t, handler, url.Values{"theme": {"dark"}})
	if response.Code != http.StatusForbidden {
		t.Errorf("non-staff status = %d, want 403", response.Code)
	}
	if cookie := themeCookieFrom(response); cookie != nil {
		t.Errorf("non-staff set a theme cookie: %+v", cookie)
	}
}

func TestThemeNextFallsBackToAdminRoot(t *testing.T) {
	cases := []struct {
		name string
		next string
		want string
	}{
		{"valid list path", "/admin/crud_product/", "/admin/crud_product/"},
		{"valid path with query", "/admin/crud_product/?q=widget", "/admin/crud_product/?q=widget"},
		{"missing", "", "/admin/"},
		{"protocol-relative host", "//evil.example/admin/", "/admin/"},
		{"absolute https", "https://evil.example/admin/", "/admin/"},
		{"absolute http", "http://evil.example/admin/", "/admin/"},
		{"backslash host", "/\\evil.example/admin/", "/admin/"},
		{"backslash after prefix", "/admin/\\..\\x", "/admin/"},
		{"encoded protocol-relative", "/admin/%2F%2Fevil.example", "/admin/"},
		{"double-encoded traversal", "/admin/%252e%252e/login/", "/admin/"},
		{"encoded traversal", "/admin/%2e%2e/x", "/admin/"},
		{"dot-dot traversal", "/admin/../login/", "/admin/"},
		{"dot-dot after segment", "/admin/x/../../x", "/admin/"},
		{"outside admin", "/login/", "/admin/"},
		{"relative without slash", "admin/crud_product/", "/admin/"},
		{"javascript scheme", "javascript:alert(1)", "/admin/"},
		{"control character", "/admin/\tx", "/admin/"},
		{"unparseable", "/admin/%zz", "/admin/"},
	}
	for _, tc := range cases {
		handler, _ := buildProductAdmin(t)
		response := postTheme(t, handler, url.Values{"theme": {"dark"}, "next": {tc.next}})
		if response.Code != http.StatusFound {
			t.Errorf("%s: status = %d, want 302", tc.name, response.Code)
			continue
		}
		if got := response.Header().Get("Location"); got != tc.want {
			t.Errorf("%s: Location = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestThemeAttributeRendersFromCookie(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	session := &http.Cookie{Name: "tango_admin_session", Value: testSessionToken}

	cases := []struct {
		name    string
		cookies []*http.Cookie
		want    string
		absent  bool
	}{
		{"explicit dark", []*http.Cookie{session, {Name: "tango_admin_theme", Value: "dark"}}, `<html lang="en" data-theme="dark">`, false},
		{"explicit light", []*http.Cookie{session, {Name: "tango_admin_theme", Value: "light"}}, `<html lang="en" data-theme="light">`, false},
		{"no cookie", []*http.Cookie{session}, `<html lang="en" data-theme=`, true},
		{"invalid cookie value", []*http.Cookie{session, {Name: "tango_admin_theme", Value: "system"}}, `<html lang="en" data-theme=`, true},
	}
	for _, tc := range cases {
		body := getWithCookies(t, handler, crudBasePath, tc.cookies...).Body.String()
		if tc.absent {
			if strings.Contains(body, tc.want) {
				t.Errorf("%s: page emitted %q", tc.name, tc.want)
			}
			continue
		}
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: page missing %q", tc.name, tc.want)
		}
	}
}

func TestLoginPageHonoursThemeCookieWithoutSwitch(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := getWithCookies(t, handler, "/admin/login/", &http.Cookie{Name: "tango_admin_theme", Value: "dark"}).Body.String()

	if !strings.Contains(body, `<html lang="en" data-theme="dark">`) {
		t.Errorf("login page did not honour the dark cookie")
	}
	if strings.Contains(body, themeBasePath) {
		t.Errorf("login page renders a theme switch")
	}
}

func TestSidebarFooterRendersThemeSwitchForStaff(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := getWithCookies(t, handler, crudBasePath, &http.Cookie{Name: "tango_admin_session", Value: testSessionToken}).Body.String()

	for _, want := range []string{
		`<form method="post" action="/admin/theme/"`,
		`<input type="hidden" name="next" value="/admin/crud_product/">`,
		`<button type="submit" name="theme" value="light"`,
		`<button type="submit" name="theme" value="dark"`,
		`<button type="submit" name="theme" value="system"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sidebar footer missing %q", want)
		}
	}
}

func TestThemeSwitchMarksTheActiveChoice(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := getWithCookies(t, handler, crudBasePath,
		&http.Cookie{Name: "tango_admin_session", Value: testSessionToken},
		&http.Cookie{Name: "tango_admin_theme", Value: "dark"},
	).Body.String()

	if !strings.Contains(body, `name="theme" value="dark" aria-pressed="true"`) {
		t.Errorf("dark is not marked as the active choice")
	}
}

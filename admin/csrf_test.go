package admin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestAdminCSRFRejectsBadOrMissingToken covers every way a state-changing
// admin request can arrive with an invalid CSRF token or cookie — a missing
// token, a syntactically fine but wrong token, a token from a different
// session, a missing token on delete, and a missing CSRF cookie on login —
// and asserts each is rejected with 403 Forbidden.
func TestAdminCSRFRejectsBadOrMissingToken(t *testing.T) {
	tests := []struct {
		name string
		// build sets up the handler and request for this case; it returns
		// the request to send plus an optional post-response check (e.g.
		// confirming a delete didn't happen despite the rejection).
		build func(t *testing.T) (http.Handler, *http.Request, func(t *testing.T))
	}{
		{
			name: "create POST with missing csrf_token is rejected",
			build: func(t *testing.T) (http.Handler, *http.Request, func(t *testing.T)) {
				handler, _ := buildProductAdmin(t)
				form := url.Values{"Name": {"Widget"}, "Price": {"9.99"}}
				request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
				return handler, request, nil
			},
		},
		{
			name: "create POST with wrong csrf_token is rejected",
			build: func(t *testing.T) (http.Handler, *http.Request, func(t *testing.T)) {
				handler, _ := buildProductAdmin(t)
				form := url.Values{"Name": {"Widget"}, "Price": {"9.99"}, "csrf_token": {"not-the-right-token"}}
				request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
				return handler, request, nil
			},
		},
		{
			name: "create POST with a token from a different session is rejected",
			build: func(t *testing.T) (http.Handler, *http.Request, func(t *testing.T)) {
				handler, _ := buildProductAdmin(t)
				// A well-formed CSRF token, but derived from a session the
				// request doesn't actually carry — must not validate
				// against testSessionToken.
				form := url.Values{
					"Name": {"Widget"}, "Price": {"9.99"},
					"csrf_token": {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				}
				request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
				return handler, request, nil
			},
		},
		{
			name: "delete POST with missing csrf_token is rejected and does not delete",
			build: func(t *testing.T) (http.Handler, *http.Request, func(t *testing.T)) {
				handler, sqlDB := buildProductAdmin(t)
				id := seedProduct(t, sqlDB, "Widget", 9.99)
				request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil)
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
				return handler, request, func(t *testing.T) {
					if !productExists(t, sqlDB, id) {
						t.Fatal("product was deleted despite a missing CSRF token")
					}
				}
			},
		},
		{
			name: "login POST with missing csrf cookie is rejected",
			build: func(t *testing.T) (http.Handler, *http.Request, func(t *testing.T)) {
				handler, _ := buildLoginTestHandler(t)
				form := url.Values{"username": {"admin"}, "password": {"correct-password"}, "csrf_token": {"whatever"}}
				request := httptest.NewRequest(http.MethodPost, "/admin/login/", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return handler, request, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, request, extraCheck := tt.build(t)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
			if extraCheck != nil {
				extraCheck(t)
			}
		})
	}
}

func TestFormPageRendersMatchingCSRFToken(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"new/", nil)

	if !strings.Contains(response.Body.String(), `name="csrf_token" value="`+testSessionCSRFToken+`"`) {
		t.Fatalf("rendered form does not carry the expected CSRF token:\n%s", response.Body.String())
	}
}

// TestLoginPageReusesExistingLoginCSRFCookie covers ensureLoginCSRFCookie's
// early-return branch: a second GET /admin/login/ carrying the cookie from
// a first visit (e.g. a reloaded or reopened login tab) must keep the same
// CSRF token rather than silently rotating it out from under an
// in-flight form.
func TestLoginPageReusesExistingLoginCSRFCookie(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	first := fetchLoginCSRF(t, handler)

	request := httptest.NewRequest(http.MethodGet, "/admin/login/", nil)
	request.AddCookie(first)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var second *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_admin_login_csrf" {
			second = cookie
		}
	}
	if second != nil && second.Value != first.Value {
		t.Fatalf("login CSRF cookie rotated on a request that already carried one: got %q, want reused %q", second.Value, first.Value)
	}
	if !strings.Contains(response.Body.String(), `value="`+first.Value+`"`) {
		t.Fatalf("rendered form does not carry the reused CSRF token:\n%s", response.Body.String())
	}
}

// TestAdminRoutesRejectUnregisteredMethods covers the router-level 405 a
// framework user hits by sending a method no admin route registers for a
// given path (e.g. PATCH) — every admin route only ever registers GET
// and/or POST, so any other method is rejected before reaching the view.
func TestAdminRoutesRejectUnregisteredMethods(t *testing.T) {
	handler, cookie := buildAuthenticatedAdminHandler(t)

	paths := []string{
		"/admin/",
		"/admin/admin_shell_user/",
		"/admin/admin_shell_user/new/",
		"/admin/admin_shell_user/42/",
		"/admin/admin_shell_user/42/delete/",
		"/admin/login/",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPatch, path, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("PATCH %s status = %d, want %d", path, response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

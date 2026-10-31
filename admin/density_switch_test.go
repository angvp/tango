package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const densityBasePath = "/admin/density/"

// postDensity sends a density POST with the session cookie. The CSRF token is
// included only when csrf is true.
func postDensity(t *testing.T, handler http.Handler, form url.Values, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	submitted := url.Values{}
	if csrf {
		submitted.Set("csrf_token", testSessionCSRFToken)
	}
	for key, values := range form {
		submitted[key] = values
	}
	request := httptest.NewRequest(http.MethodPost, densityBasePath, strings.NewReader(submitted.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func densityCookieFrom(response *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_admin_density" {
			return cookie
		}
	}
	return nil
}

func TestDensityPostStoresCompactAndComfortable(t *testing.T) {
	for _, density := range []string{"compact", "comfortable"} {
		handler, _ := buildProductAdmin(t)
		response := postDensity(t, handler, url.Values{"density": {density}, "next": {"/admin/crud_product/"}}, true)

		if response.Code != http.StatusFound {
			t.Fatalf("%s: status = %d, want 302", density, response.Code)
		}
		if got := response.Header().Get("Location"); got != "/admin/crud_product/" {
			t.Errorf("%s: redirect = %q, want the next path", density, got)
		}
		cookie := densityCookieFrom(response)
		if cookie == nil || cookie.Value != density {
			t.Fatalf("%s: density cookie = %+v, want value %q", density, cookie, density)
		}
		if !cookie.HttpOnly || cookie.Path != "/admin/" {
			t.Errorf("%s: density cookie HttpOnly=%v Path=%q, want HttpOnly on /admin/", density, cookie.HttpOnly, cookie.Path)
		}
	}
}

func TestDensityPostRefusesAMissingCSRFToken(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	response := postDensity(t, handler, url.Values{"density": {"compact"}}, false)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if densityCookieFrom(response) != nil {
		t.Error("a request without a CSRF token set the density cookie")
	}
}

func TestDensityPostRefusesAnUnknownValue(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	response := postDensity(t, handler, url.Values{"density": {"tiny"}}, true)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if densityCookieFrom(response) != nil {
		t.Error("an unknown density value set the density cookie")
	}
}

func TestDensityPostRedirectsAnUnsafeNextToTheAdminRoot(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	response := postDensity(t, handler, url.Values{"density": {"compact"}, "next": {"https://example.com/"}}, true)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if got := response.Header().Get("Location"); got != "/admin/" {
		t.Errorf("redirect = %q, want /admin/", got)
	}
}

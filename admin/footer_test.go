package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSidebarFooterShowsTheSignedInUserAndALogoutForm(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()

	if strings.Contains(body, "Basic Auth") {
		t.Errorf("footer still claims Basic Auth:\n%s", body)
	}
	for _, want := range []string{
		"Signed in as admin",
		`<form method="post" action="/admin/logout/"`,
		`<input type="hidden" name="csrf_token" value="` + testSessionCSRFToken + `">`,
		"Log out",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("footer missing %q:\n%s", want, body)
		}
	}
}

func TestLogoutRequiresTheSessionCSRFToken(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)

	cases := []struct {
		name string
		form url.Values
	}{
		{"missing token", url.Values{}},
		{"wrong token", url.Values{"csrf_token": {"not-the-token"}}},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(http.MethodPost, "/admin/logout/", strings.NewReader(tc.form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", tc.name, response.Code)
		}
		if got := response.Header().Get("Set-Cookie"); strings.Contains(got, "Max-Age=0") || strings.Contains(got, "Max-Age=-1") {
			t.Errorf("%s: session cookie was cleared: %q", tc.name, got)
		}
		var sessions int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_session`).Scan(&sessions); err != nil || sessions != 1 {
			t.Errorf("%s: session rows = %d (err %v), want 1 still stored", tc.name, sessions, err)
		}
	}

	valid := doRequest(t, handler, http.MethodPost, "/admin/logout/", nil)
	if valid.Code != http.StatusFound || valid.Header().Get("Location") != "/admin/login/" {
		t.Fatalf("valid logout = %d %q, want 302 to /admin/login/", valid.Code, valid.Header().Get("Location"))
	}
	var sessions int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_session`).Scan(&sessions); err != nil || sessions != 0 {
		t.Errorf("session rows after valid logout = %d (err %v), want 0", sessions, err)
	}
}

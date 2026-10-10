package admin_test

import (
	"net/http"
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

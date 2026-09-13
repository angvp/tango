package admin_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func buildBrandedProductAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	return buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
	}, admin.WithBranding(admin.Branding{Name: "Acme Admin", LogoURL: "/static/logo.png"}))
}

func TestDefaultBrandingRendersGenericSidebarTitle(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, "GET", crudBasePath, nil)
	if !strings.Contains(response.Body.String(), "tanGO Admin") {
		t.Fatalf("expected the generic default brand title, got:\n%s", response.Body.String())
	}
}

func TestWithBrandingReplacesSidebarNameAndLogo(t *testing.T) {
	handler, _ := buildBrandedProductAdmin(t)

	response := doRequest(t, handler, "GET", crudBasePath, nil)
	body := response.Body.String()

	if strings.Contains(body, `sidebar-brand-text">tanGO Admin<`) {
		t.Fatalf("expected the sidebar's generic brand text to be replaced, but it's still present:\n%s", body)
	}
	if !strings.Contains(body, "Acme Admin") {
		t.Fatalf("expected the configured brand name, got:\n%s", body)
	}
	if !strings.Contains(body, `src="/static/logo.png"`) {
		t.Fatalf("expected the configured brand logo, got:\n%s", body)
	}
}

package admin_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var navScriptTag = regexp.MustCompile(`<script src="(/admin/_assets/nav\.[0-9a-f]{16}\.js)" defer></script>`)

// The sidebar behaviour is a hashed, deferred script that the layout
// references. The menu button and the backdrop carry data attributes, not
// inline handlers, so a strict policy can run the page.
func TestAdminLayoutLoadsItsSidebarScriptFromAHashedAsset(t *testing.T) {
	handler := buildAdminHandler(t)
	page := doRequest(t, handler, http.MethodGet, "/admin/login/", nil).Body.String()

	match := navScriptTag.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("login page references no hashed nav.js script:\n%s", page)
	}
	if strings.Contains(page, " onclick=") {
		t.Errorf("layout still carries an inline onclick handler")
	}
	if !strings.Contains(page, `data-admin-nav-close`) {
		t.Errorf("backdrop lacks the data attribute the sidebar script binds to")
	}
	handlerWithList, _ := buildProductAdmin(t)
	list := doRequest(t, handlerWithList, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(list, `data-admin-nav-open`) {
		t.Errorf("menu button lacks the data attribute the sidebar script binds to")
	}

	asset := doRequest(t, handler, http.MethodGet, match[1], nil)
	if asset.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", match[1], asset.Code)
	}
	wantHeaders := map[string]string{
		"Content-Type":           "text/javascript; charset=utf-8",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "public, max-age=31536000, immutable",
	}
	for name, want := range wantHeaders {
		if got := asset.Header().Get(name); got != want {
			t.Errorf("%s: %s = %q, want %q", match[1], name, got, want)
		}
	}
}

package admin_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
)

func TestBulkOutcomeReadsAsAPlainCount(t *testing.T) {
	handler, _ := buildMessageBookAdmin(t)
	for _, tt := range []struct{ query, want string }{
		{"?deleted=3", "3 books deleted"},
		{"?deleted=1", "1 book deleted"},
	} {
		body := doRequest(t, handler, http.MethodGet, messageBookBasePath+tt.query, nil).Body.String()
		if !strings.Contains(body, tt.want) {
			t.Fatalf("list %s lacks %q", tt.query, tt.want)
		}
		if strings.Contains(body, "rows") {
			t.Fatalf("list %s still says rows", tt.query)
		}
	}
}

func TestBulkRejectionsSayNothingWasDeleted(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")
	const bulkPath = "/admin/message_book/delete-selected/"

	missing := doRequest(t, handler, http.MethodPost, bulkPath, bulkForm(1, 999))
	if !strings.Contains(missing.Body.String(), "Nothing was deleted. One selected book no longer exists.") {
		t.Fatalf("missing-row rejection lacks the plain copy: %.300s", missing.Body.String())
	}

	invalid := doRequest(t, handler, http.MethodPost, bulkPath, bulkForm(2, 1))
	if !strings.Contains(invalid.Body.String(), "Nothing was deleted. The selection is not valid.") {
		t.Fatalf("invalid-selection rejection lacks the plain copy: %.300s", invalid.Body.String())
	}
}

func TestEmptyStatesTeachTheNextAction(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)

	empty := doRequest(t, handler, http.MethodGet, messageBookBasePath, nil).Body.String()
	for _, want := range []string{"No books yet", "Add the first book to see it here."} {
		if !strings.Contains(empty, want) {
			t.Fatalf("no-rows state lacks %q", want)
		}
	}

	seedMessageBook(t, sqlDB, "Dune")
	search := doRequest(t, handler, http.MethodGet, messageBookBasePath+"?q=lemon", nil).Body.String()
	if !strings.Contains(search, "No books match “lemon”.") {
		t.Fatalf("no-search-match state lacks its copy")
	}
}

func TestDisplayNameFallsBackToHumanizedGoName(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(body, "No crud products yet") {
		t.Fatalf("empty state lacks the humanized plural name")
	}
	if strings.Contains(body, "No crudProduct") {
		t.Fatalf("the raw Go type name reached the empty state")
	}
}

func TestHostCatalogOverridesOneMessageKey(t *testing.T) {
	i18ntest.Isolate(t)
	handler, _ := buildMessageBookAdmin(t, admin.WithMiddleware(adminI18NMiddleware()))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.list.empty_title": "Sin %s todavía",
	})
	body := doRequest(t, handler, http.MethodGet, messageBookBasePath, nil).Body.String()
	if !strings.Contains(body, "Sin books todavía") {
		t.Fatalf("the host wording did not reach the empty state")
	}
	if !strings.Contains(body, "Add the first book to see it here.") {
		t.Fatalf("a key the host did not override lost its English fallback")
	}
}

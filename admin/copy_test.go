package admin_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
)

// Reads the count wording from the page a real bulk delete redirects to, where
// the outcome is carried as a flash message.
func TestBulkOutcomeReadsAsAPlainCount(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	ids := []int64{seedMessageBook(t, sqlDB, "Alpha"), seedMessageBook(t, sqlDB, "Beta"), seedMessageBook(t, sqlDB, "Gamma")}
	posted := signedRequest(handler, http.MethodPost, messageBookBasePath+"delete-selected/", testSessionToken, bulkForm(3, ids...))
	list := signedRequest(handler, http.MethodGet, messageBookBasePath, testSessionToken, nil, flashSetBy(posted))
	if !strings.Contains(list.Body.String(), "3 books deleted") {
		t.Fatalf("list lacks %q", "3 books deleted")
	}
	if strings.Contains(list.Body.String(), "rows") {
		t.Fatal("list still says rows")
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
	if strings.Contains(body, "crudProduct") {
		t.Fatalf("the raw Go type name reached the page")
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

// TestFilteredEmptyStateNamesTheFilters covers the empty state of a changelist
// whose filters match nothing.
func TestFilteredEmptyStateNamesTheFilters(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Draft Only", false, author)

	body := doRequest(t, handler, http.MethodGet, flBookPath+"?filter_published=yes", nil).Body.String()
	if !strings.Contains(body, "No fl books match the selected filters.") {
		t.Fatalf("the filtered empty state lacks its copy")
	}
}

// TestDeleteConfirmationNamesTheRowAndTheAction covers the single-row delete
// page and the bulk dialog: the row is named by its label, the button names
// the action and the noun, and neither shows the primary key alone.
func TestDeleteConfirmationNamesTheRowAndTheAction(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	id := seedMessageBook(t, sqlDB, "Dune")

	page := doRequest(t, handler, http.MethodGet, messageBookBasePath+strconv.FormatInt(id, 10)+"/delete/", nil).Body.String()
	for _, want := range []string{
		`<span class="font-medium text-ink">Dune</span>`,
		">Delete book</button>",
		">Cancel</a>",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("delete page lacks %q", want)
		}
	}
	if strings.Contains(page, "Confirm delete") {
		t.Fatal("delete page still uses the generic confirm label")
	}

	list := doRequest(t, handler, http.MethodGet, messageBookBasePath, nil).Body.String()
	for _, want := range []string{
		`data-msg-confirm-one="Delete 1 book"`,
		`data-msg-confirm-many="Delete {count} books"`,
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("bulk dialog lacks %q", want)
		}
	}
}

// TestNoRawGoTypeNameOnAnyPage applies the raw-name check to every page a
// messageBook renders: the changelist, both forms, the delete page and the
// bulk rejection.
func TestNoRawGoTypeNameOnAnyPage(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	id := seedMessageBook(t, sqlDB, "Dune")
	idText := strconv.FormatInt(id, 10)

	responses := map[string]string{
		"list":        doRequest(t, handler, http.MethodGet, messageBookBasePath, nil).Body.String(),
		"create":      doRequest(t, handler, http.MethodGet, messageBookBasePath+"new/", nil).Body.String(),
		"edit":        doRequest(t, handler, http.MethodGet, messageBookBasePath+idText+"/", nil).Body.String(),
		"delete":      doRequest(t, handler, http.MethodGet, messageBookBasePath+idText+"/delete/", nil).Body.String(),
		"bulk reject": doRequest(t, handler, http.MethodPost, "/admin/message_book/delete-selected/", bulkForm(1, 999)).Body.String(),
	}
	for name, body := range responses {
		if strings.Contains(body, "messageBook") {
			t.Fatalf("%s page shows the raw Go type name", name)
		}
	}
}

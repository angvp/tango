package admin_test

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func idString(id int64) string { return strconv.FormatInt(id, 10) }

func TestBulkDeleteListRendersSelectionControlsOnlyWhenEnabled(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
		id := seedProduct(t, sqlDB, "Widget", 3)
		body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()

		for _, want := range []string{
			`data-bulk-delete`,
			`action="` + crudBasePath + `delete-selected/"`,
			`name="csrf_token"`,
			`name="return_to" value="` + crudBasePath + `?page=1"`,
			`<dialog`,
			`data-bulk-select-all`,
			`data-bulk-pk="` + idString(id) + `"`,
			`data-bulk-identity="Widget"`,
			`/admin/_assets/bulk.`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("list lacks %q", want)
			}
		}
		// Controls start hidden so a browser without JavaScript never shows
		// an inert delete control.
		for _, marker := range []string{`data-bulk-bar`, `data-bulk-cell`} {
			if !regexp.MustCompile(`(?s)<[^>]*` + marker + `[^>]*\bhidden\b[^>]*>|<[^>]*\bhidden\b[^>]*` + marker + `[^>]*>`).MatchString(body) {
				t.Errorf("%s is not hidden by default", marker)
			}
		}
	})
	t.Run("disabled", func(t *testing.T) {
		handler, sqlDB := buildProductAdmin(t)
		seedProduct(t, sqlDB, "Widget", 3)
		body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
		for _, unwanted := range []string{`data-bulk`, `delete-selected`, `<dialog`, `/admin/_assets/bulk.`} {
			if strings.Contains(body, unwanted) {
				t.Errorf("list carries %q for a model that did not opt in", unwanted)
			}
		}
	})
}

func TestBulkDeleteSelectAllLabelNamesThePageRowCount(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	for i := 0; i < 3; i++ {
		seedProduct(t, sqlDB, "Item", float64(i))
	}
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(body, `aria-label="Select all 3 rows on this page"`) {
		t.Errorf("select-all label does not carry the row count")
	}
}

func TestBulkDeleteDialogOpensOnTheSafeButton(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	seedProduct(t, sqlDB, "Widget", 3)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !regexp.MustCompile(`<button[^>]*data-bulk-cancel[^>]*\bautofocus\b|<button[^>]*\bautofocus\b[^>]*data-bulk-cancel`).MatchString(body) {
		t.Errorf("the dialog must open with Cancel focused, so a stray Enter cannot delete")
	}
	if regexp.MustCompile(`<button[^>]*data-bulk-confirm[^>]*\bautofocus\b`).MatchString(body) {
		t.Errorf("Confirm delete must not take the initial focus")
	}
}

func TestBulkDeleteListReturnToKeepsTheCurrentView(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	for i := 0; i < 30; i++ {
		seedProduct(t, sqlDB, "Item", float64(i))
	}
	body := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=Item&page=2", nil).Body.String()
	want := `name="return_to" value="` + crudBasePath + `?page=2&amp;q=Item"`
	if !strings.Contains(body, want) {
		t.Fatalf("list lacks %q", want)
	}
}

func TestBulkDeleteIdentityIsTheFirstListDisplayValueOrTheKey(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	named := seedProduct(t, sqlDB, `<b>"x"</b>`, 1)
	blank := seedProduct(t, sqlDB, "", 2)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()

	if !strings.Contains(body, `data-bulk-pk="`+idString(named)+`" data-bulk-identity="&lt;b&gt;&#34;x&#34;&lt;/b&gt;"`) {
		t.Errorf("identity of the named row is not escaped text of its first column")
	}
	if !strings.Contains(body, `data-bulk-pk="`+idString(blank)+`" data-bulk-identity="`+idString(blank)+`"`) {
		t.Errorf("a blank first column must fall back to the key")
	}
}

func TestBulkDeleteIdentityFallsBackToTheKeyForABlankValue(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	blank := seedProduct(t, sqlDB, "   ", 2)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(body, `data-bulk-pk="`+idString(blank)+`" data-bulk-identity="`+idString(blank)+`"`) {
		t.Errorf("a whitespace-only first column must fall back to the key")
	}
}

func TestBulkDeleteListClampsAPastTheEndPageInReturnTo(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	seedProduct(t, sqlDB, "Only", 1)
	body := doRequest(t, handler, http.MethodGet, crudBasePath+"?page=9", nil).Body.String()
	if !strings.Contains(body, `name="return_to" value="`+crudBasePath+`?page=1"`) {
		t.Errorf("return_to must carry the clamped page")
	}
}

// An outcome is announced only from the flash cookie, so a deleted parameter
// on the list URL announces nothing. The announcement itself is covered by the
// flash tests.
func TestBulkDeleteListIgnoresADeletedQueryParameter(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	banner := regexp.MustCompile(`role="alert"|alert-success`)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))
	if shown := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash); !banner.MatchString(shown.Body.String()) {
		t.Fatal("a genuine outcome was not shown; the query-only rejections below would prove nothing")
	}
	for _, query := range []string{"deleted=1", "deleted=2", "deleted=25", "deleted=0"} {
		body := doRequest(t, handler, http.MethodGet, crudBasePath+"?"+query, nil).Body.String()
		if banner.MatchString(body) {
			t.Errorf("%s: an outcome banner was shown from the query", query)
		}
	}
}

func TestBulkDeleteScriptAssetIsServed(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, bulkProductOptions())
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	match := regexp.MustCompile(`src="(/admin/_assets/bulk\.[0-9a-f]+\.js)"`).FindStringSubmatch(body)
	if match == nil {
		t.Fatal("list does not reference the bulk script")
	}
	response := doRequest(t, handler, http.MethodGet, match[1], nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("asset status %d type %q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), "showModal") {
		t.Errorf("asset does not open the confirmation dialog")
	}
}

func TestBulkDeleteBarNamesItsReasonOnTheButton(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	seedProduct(t, sqlDB, "Widget", 3)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()

	button := regexp.MustCompile(`<button[^>]*data-bulk-open[^>]*>`).FindString(body)
	if !strings.Contains(button, `aria-describedby="bulk-count"`) {
		t.Fatalf("the Delete selected button is not described by the count slot: %s", button)
	}
	reason := regexp.MustCompile(`id="bulk-count"[^>]*>([^<]*)<`).FindStringSubmatch(body)
	if reason == nil || reason[1] != "Select rows to delete them." {
		t.Fatalf("the count slot does not carry the visible reason: %v", reason)
	}
}

func TestBulkDeleteDialogCopyNamesTheCountAndTheCascade(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	seedProduct(t, sqlDB, "Widget", 3)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	for _, want := range []string{
		`data-msg-title-one="Delete 1 crud product?"`,
		`data-msg-title-many="Delete {count} crud products?"`,
		`data-msg-confirm-one="Delete 1 crud product"`,
		`data-msg-confirm-many="Delete {count} crud products"`,
		`Rows that reference these rows are deleted with them. This action cannot be undone.`,
		`data-bulk-limit="10"`,
		`data-msg-more-one="and 1 more"`,
		`data-msg-more-many="and {count} more"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dialog lacks %q", want)
		}
	}
}

func TestBulkDeleteSelectAllHasAVisiblePhoneControl(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	seedProduct(t, sqlDB, "Widget", 3)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if got := strings.Count(body, `data-bulk-select-all`); got != 2 {
		t.Fatalf("select-all controls = %d, want 2 (table header and phone bar)", got)
	}
	if !strings.Contains(body, `Select all on this page`) {
		t.Errorf("the phone select-all control has no visible label")
	}
}

package admin_test

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// flAuthor and flBook are the changelist-filter fixtures: flBook carries a
// boolean and a foreign key, and flAuthor has no Filters so the tests can
// prove an unfiltered model ignores filter parameters.
type flAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

type flBook struct {
	ID        int64 `tango:"pk"`
	Title     string
	Published bool
	AuthorID  int64 `tango:"fk=flAuthor"`
}

const flBookPath = "/admin/fl_book/"

// buildBookAdmin wires flAuthor (labelled by Name) and flBook with bookOptions
// through the real admin handler on a fresh migrated database.
func buildBookAdmin(t *testing.T, bookOptions admin.Options) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(flAuthor{}); err != nil {
		t.Fatalf("register flAuthor: %v", err)
	}
	if err := registry.Admin().Register(flAuthor{}, admin.Options{
		ListDisplay: []string{"Name"},
		Label:       "Name",
	}); err != nil {
		t.Fatalf("register admin flAuthor: %v", err)
	}
	if err := registry.Models().Register(flBook{}); err != nil {
		t.Fatalf("register flBook: %v", err)
	}
	if err := registry.Admin().Register(flBook{}, bookOptions); err != nil {
		t.Fatalf("register admin flBook: %v", err)
	}

	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}

	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB
}

func seedFilterAuthor(t *testing.T, sqlDB *sql.DB, name string) int64 {
	t.Helper()
	return insertReturningID(t, sqlDB, `INSERT INTO fl_author (name) VALUES ($1) RETURNING id`, name)
}

func seedBook(t *testing.T, sqlDB *sql.DB, title string, published bool, authorID int64) int64 {
	t.Helper()
	return insertReturningID(t, sqlDB,
		`INSERT INTO fl_book (title, published, author_id) VALUES ($1, $2, $3) RETURNING id`,
		title, published, authorID)
}

func bookFilterOptions() admin.Options {
	return admin.Options{
		ListDisplay: []string{"Title", "Published", "AuthorID"},
		Search:      []string{"Title"},
		Ordering:    []string{"Title"},
		Filters:     []string{"Published", "AuthorID"},
	}
}

// listURL builds a changelist URL for path with the given query values.
func listURL(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}

func TestBooleanFilterNarrowsChangelist(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Live Title", true, author)
	seedBook(t, sqlDB, "Draft Title", false, author)

	t.Run("yes shows only published rows and selects yes", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath+"?filter_published=yes", nil).Body.String()
		if !strings.Contains(body, "Live Title") || strings.Contains(body, "Draft Title") {
			t.Fatalf("yes should show only the published row:\n%s", body)
		}
		if !strings.Contains(body, `<option value="yes" selected>`) {
			t.Fatalf("yes option should be selected:\n%s", body)
		}
		if !strings.Contains(body, "1 fl book") {
			t.Fatalf("count should reflect the filtered set:\n%s", body)
		}
	})

	t.Run("no shows only unpublished rows", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath+"?filter_published=no", nil).Body.String()
		if strings.Contains(body, "Live Title") || !strings.Contains(body, "Draft Title") {
			t.Fatalf("no should show only the draft row:\n%s", body)
		}
	})

	t.Run("any and a missing parameter show every row", func(t *testing.T) {
		for _, path := range []string{flBookPath + "?filter_published=any", flBookPath} {
			body := doRequest(t, handler, "GET", path, nil).Body.String()
			if !strings.Contains(body, "Live Title") || !strings.Contains(body, "Draft Title") {
				t.Fatalf("%s should show every row:\n%s", path, body)
			}
		}
	})

	t.Run("a malformed value is ignored and shows every row", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath+"?filter_published=maybe", nil).Body.String()
		if !strings.Contains(body, "Live Title") || !strings.Contains(body, "Draft Title") {
			t.Fatalf("malformed value should be ignored:\n%s", body)
		}
		if strings.Contains(body, `value="maybe"`) {
			t.Fatalf("ignored value must not appear in page state:\n%s", body)
		}
	})
}

func TestForeignKeyFilterOffersLabelledAuthorsAndNarrowsRows(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	ada := seedFilterAuthor(t, sqlDB, "Ada Lovelace")
	grace := seedFilterAuthor(t, sqlDB, "Grace Hopper")
	seedBook(t, sqlDB, "Engine Notes", true, ada)
	seedBook(t, sqlDB, "Compiler Notes", false, grace)

	t.Run("options are labelled and ordered by primary key, with any first", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
		assertOrder(t, body,
			`<option value="any" selected>Any</option>`,
			`<option value="`+strconv.FormatInt(ada, 10)+`">Ada Lovelace</option>`,
			`<option value="`+strconv.FormatInt(grace, 10)+`">Grace Hopper</option>`,
		)
		if !strings.Contains(body, `<label for="filter-author_id"`) {
			t.Fatalf("FK filter must be labelled:\n%s", body)
		}
	})

	t.Run("selecting an author narrows rows and selects that option", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath+"?filter_author_id="+strconv.FormatInt(grace, 10), nil).Body.String()
		if strings.Contains(body, "Engine Notes") || !strings.Contains(body, "Compiler Notes") {
			t.Fatalf("author filter should keep only Grace's book:\n%s", body)
		}
		if !strings.Contains(body, `<option value="`+strconv.FormatInt(grace, 10)+`" selected>`) {
			t.Fatalf("Grace's option should be selected:\n%s", body)
		}
		if !strings.Contains(body, "1 fl book") {
			t.Fatalf("count should reflect the filtered set:\n%s", body)
		}
	})

	t.Run("a nonexistent related ID is ignored and not reflected in page state", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath+"?filter_author_id=999999", nil).Body.String()
		if !strings.Contains(body, "Engine Notes") || !strings.Contains(body, "Compiler Notes") {
			t.Fatalf("unknown author should be ignored:\n%s", body)
		}
		if strings.Contains(body, "999999") {
			t.Fatalf("ignored ID must not appear in page state:\n%s", body)
		}
	})

	t.Run("a malformed ID is ignored", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath+"?filter_author_id=ada", nil).Body.String()
		if !strings.Contains(body, "Engine Notes") || !strings.Contains(body, "Compiler Notes") {
			t.Fatalf("malformed author ID should be ignored:\n%s", body)
		}
	})
}

func TestForeignKeyFilterOverCapExplainsItselfAndOffersNoChoices(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	var last int64
	for i := 0; i <= 100; i++ {
		last = seedFilterAuthor(t, sqlDB, "Author "+strconv.Itoa(i))
	}
	seedBook(t, sqlDB, "Orphan Notes", true, last)

	body := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
	if !strings.Contains(body, "too many values to list") {
		t.Fatalf("over-cap filter should explain itself:\n%s", body)
	}
	if strings.Contains(body, `<select id="filter-author_id"`) {
		t.Fatalf("over-cap filter should offer no select:\n%s", body)
	}
	if strings.Contains(body, `<label for="filter-author_id"`) {
		t.Fatalf("over-cap filter has no control, so its label must not point at one:\n%s", body)
	}
	if strings.Contains(body, `<span class="field-caption">Author`) {
		t.Fatalf("over-cap filter must not leave its label standing alone as a span:\n%s", body)
	}

	// A bookmarked value still filters: the cap limits listing, not filtering.
	other := seedFilterAuthor(t, sqlDB, "Someone Else")
	seedBook(t, sqlDB, "Other Notes", true, other)
	id := strconv.FormatInt(last, 10)
	body = doRequest(t, handler, "GET", flBookPath+"?filter_author_id="+id, nil).Body.String()
	if !strings.Contains(body, "Orphan Notes") || strings.Contains(body, "Other Notes") {
		t.Fatalf("a valid over-cap value should filter the list:\n%s", body)
	}
	if !strings.Contains(body, `<input type="hidden" name="filter_author_id" value="`+id+`">`) {
		t.Fatalf("the applied value should survive a form resubmit:\n%s", body)
	}
	if !strings.Contains(body, "Author 100") {
		t.Fatalf("the applied row's label should be shown:\n%s", body)
	}
	if strings.Contains(body, `<select id="filter-author_id"`) {
		t.Fatalf("applying a value must not bring back the select:\n%s", body)
	}

	for _, bad := range []string{"999999", "ada", "-1"} {
		body = doRequest(t, handler, "GET", flBookPath+"?filter_author_id="+bad, nil).Body.String()
		if !strings.Contains(body, "Orphan Notes") || !strings.Contains(body, "Other Notes") || strings.Contains(body, `filter_author_id=`+bad) {
			t.Fatalf("over-cap value %q should be ignored, not applied or reflected:\n%s", bad, body)
		}
	}
}

// pageLinkHrefs returns the decoded href of every changelist link in body,
// with its HTML entities unescaped, so tests can parse the query it carries.
func pageLinkHrefs(t *testing.T, body string) []string {
	t.Helper()
	var hrefs []string
	for _, match := range pageLinkPattern.FindAllStringSubmatch(body, -1) {
		hrefs = append(hrefs, html.UnescapeString(match[1]))
	}
	return hrefs
}

var pageLinkPattern = regexp.MustCompile(`<a href="([^"]*)" class="page-link"`)

func TestFiltersAndSearchComposeWithCountsAndPagination(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	other := seedFilterAuthor(t, sqlDB, "Grace")
	for i := 0; i < 30; i++ {
		seedBook(t, sqlDB, fmt.Sprintf("Engine & Co %02d", i), true, author)
	}
	seedBook(t, sqlDB, "Engine Draft", false, author)
	seedBook(t, sqlDB, "Engine Other", true, other)
	seedBook(t, sqlDB, "Plain Published", true, author)

	t.Run("filters AND with search and keep the filtered count", func(t *testing.T) {
		values := url.Values{"q": {"Engine"}, "filter_author_id": {strconv.FormatInt(author, 10)}, "filter_published": {"yes"}}
		body := doRequest(t, handler, "GET", listURL(flBookPath, values), nil).Body.String()
		if strings.Contains(body, "Engine Draft") || strings.Contains(body, "Engine Other") || strings.Contains(body, "Plain Published") {
			t.Fatalf("only published Ada books matching Engine should show:\n%s", body)
		}
		if !strings.Contains(body, "30 fl books") {
			t.Fatalf("count should be the filtered total (30):\n%s", body)
		}
	})

	t.Run("pagination links carry the search and applied filters, correctly encoded", func(t *testing.T) {
		values := url.Values{"q": {"Engine & Co"}, "filter_published": {"yes"}}
		body := doRequest(t, handler, "GET", listURL(flBookPath, values), nil).Body.String()
		hrefs := pageLinkHrefs(t, body)
		if len(hrefs) == 0 {
			t.Fatalf("expected pagination links:\n%s", body)
		}
		for _, href := range hrefs {
			query, err := url.ParseQuery(strings.TrimPrefix(href, "?"))
			if err != nil {
				t.Fatalf("href %q is not a valid query: %v", href, err)
			}
			if got := query.Get("q"); got != "Engine & Co" {
				t.Fatalf("href %q carries q=%q, want %q", href, got, "Engine & Co")
			}
			if got := query.Get("filter_published"); got != "yes" {
				t.Fatalf("href %q carries filter_published=%q, want yes", href, got)
			}
		}
	})

	t.Run("a page of filtered results keeps the filter on its links", func(t *testing.T) {
		values := url.Values{"filter_published": {"yes"}, "page": {"2"}}
		body := doRequest(t, handler, "GET", listURL(flBookPath, values), nil).Body.String()
		if strings.Contains(body, "Engine Draft") {
			t.Fatalf("page 2 must keep the published filter:\n%s", body)
		}
		if !strings.Contains(body, "Showing 26 to 32 of 32 fl books") {
			t.Fatalf("count should be the filtered total (32):\n%s", body)
		}
	})
}

func TestChangelistControlsAreLabelledForAssistiveTech(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Engine Notes", true, author)

	body := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
	for _, want := range []string{
		`<label for="list-search"`,
		`<input id="list-search" type="search" name="q"`,
		`<label for="filter-published"`,
		`<select id="filter-published" name="filter_published"`,
		`<label for="filter-author_id"`,
		`<select id="filter-author_id" name="filter_author_id"`,
		`<caption class="sr-only">Fl books</caption>`,
		`<th scope="col">Title</th>`,
		`<th scope="col" class="cell-actions">Actions</th>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("changelist should contain %q:\n%s", want, body)
		}
	}
}

func TestUnfilteredModelIgnoresFilterParametersAndShowsNoFilters(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	seedFilterAuthor(t, sqlDB, "Ada")
	seedFilterAuthor(t, sqlDB, "Grace")

	plain := doRequest(t, handler, "GET", "/admin/fl_author/", nil).Body.String()
	withParams := doRequest(t, handler, "GET", listURL("/admin/fl_author/", url.Values{
		"filter_name":      {"Ada"},
		"filter_published": {"yes"},
		"filter_id":        {"1"},
	}), nil).Body.String()

	if strings.Contains(plain, "<select") {
		t.Fatalf("a model without Filters must render no filter controls:\n%s", plain)
	}
	if plain != withParams {
		t.Fatalf("filter parameters on a model without Filters must not change its page")
	}
}

func TestUnlistedFieldParametersAreIgnoredAndEmptyStateExplainsFilters(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Draft Only", false, author)

	body := doRequest(t, handler, "GET", flBookPath+"?filter_title=Draft+Only", nil).Body.String()
	if !strings.Contains(body, "Draft Only") || strings.Contains(body, `value="Draft Only"`) {
		t.Fatalf("a field that is not in Filters must be ignored and not reflected:\n%s", body)
	}

	body = doRequest(t, handler, "GET", flBookPath+"?filter_published=yes", nil).Body.String()
	if !strings.Contains(body, "No fl books match the selected filters.") {
		t.Fatalf("an empty filtered list should say the filters matched nothing:\n%s", body)
	}
}

func TestResultLineStatesTheRangeOfTheRowsOnThePage(t *testing.T) {
	t.Run("a full first page names the first and last row and the total", func(t *testing.T) {
		handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
		author := seedFilterAuthor(t, sqlDB, "Ada")
		for i := 0; i < 60; i++ {
			seedBook(t, sqlDB, fmt.Sprintf("Row %02d", i), true, author)
		}
		body := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
		if !strings.Contains(body, "Showing 1 to 25 of 60 fl books") {
			t.Fatalf("first page should state its range:\n%s", body)
		}
	})

	t.Run("a partial last page ends at the total", func(t *testing.T) {
		handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
		author := seedFilterAuthor(t, sqlDB, "Ada")
		for i := 0; i < 60; i++ {
			seedBook(t, sqlDB, fmt.Sprintf("Row %02d", i), true, author)
		}
		body := doRequest(t, handler, "GET", flBookPath+"?page=3", nil).Body.String()
		if !strings.Contains(body, "Showing 51 to 60 of 60 fl books") {
			t.Fatalf("last page should state rows 51 to 60:\n%s", body)
		}
	})

	t.Run("an out-of-range page is clamped and states the last page's range", func(t *testing.T) {
		handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
		author := seedFilterAuthor(t, sqlDB, "Ada")
		for i := 0; i < 60; i++ {
			seedBook(t, sqlDB, fmt.Sprintf("Row %02d", i), true, author)
		}
		body := doRequest(t, handler, "GET", flBookPath+"?page=99", nil).Body.String()
		if !strings.Contains(body, "Showing 51 to 60 of 60 fl books") {
			t.Fatalf("clamped page should state rows 51 to 60:\n%s", body)
		}
	})

	t.Run("one row uses the singular noun", func(t *testing.T) {
		handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
		author := seedFilterAuthor(t, sqlDB, "Ada")
		seedBook(t, sqlDB, "Only Row", true, author)
		body := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
		if !strings.Contains(body, ">1 fl book<") {
			t.Fatalf("one row should read 1 fl book:\n%s", body)
		}
		if strings.Contains(body, "Showing") {
			t.Fatalf("a single page should not use the range sentence:\n%s", body)
		}
	})
}

func TestAppliedFilterSummaryNamesEachFilterAndClearsThem(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	ada := seedFilterAuthor(t, sqlDB, "Ada Lovelace")
	seedBook(t, sqlDB, "Engine & Co Notes", true, ada)
	seedBook(t, sqlDB, "Engine Draft", false, ada)

	t.Run("each applied filter is named with its value", func(t *testing.T) {
		values := url.Values{"q": {"Engine & Co"}, "filter_published": {"yes"}, "filter_author_id": {strconv.FormatInt(ada, 10)}}
		body := doRequest(t, handler, "GET", listURL(flBookPath, values), nil).Body.String()
		for _, want := range []string{"Filters applied:", "Published: Yes", "Author ID: Ada Lovelace"} {
			if !strings.Contains(body, want) {
				t.Fatalf("summary should contain %q:\n%s", want, body)
			}
		}
	})

	t.Run("Clear filters keeps the search and drops the filters", func(t *testing.T) {
		values := url.Values{"q": {"Engine & Co"}, "filter_published": {"yes"}}
		body := doRequest(t, handler, "GET", listURL(flBookPath, values), nil).Body.String()
		if got := clearLinkHref(t, body, "Clear filters"); got != flBookPath+"?q=Engine+%26+Co" {
			t.Fatalf("Clear filters should keep the search and drop the filters, got %q:\n%s", got, body)
		}
	})

	t.Run("Clear search keeps the filters and drops the search", func(t *testing.T) {
		values := url.Values{"q": {"Engine & Co"}, "filter_published": {"yes"}}
		body := doRequest(t, handler, "GET", listURL(flBookPath, values), nil).Body.String()
		if got := clearLinkHref(t, body, "Clear search"); got != flBookPath+"?filter_published=yes" {
			t.Fatalf("Clear search should keep the filters and drop the search, got %q:\n%s", got, body)
		}
	})

	t.Run("the summary and Clear filters remain when no row matches", func(t *testing.T) {
		body := doRequest(t, handler, "GET", listURL(flBookPath, url.Values{"q": {"Nothing Matches"}, "filter_published": {"no"}}), nil).Body.String()
		if !strings.Contains(body, "Published: No") || !strings.Contains(body, ">Clear filters</a>") {
			t.Fatalf("an empty result should still offer the summary and Clear filters:\n%s", body)
		}
	})

	t.Run("no criteria means no summary and no clear link", func(t *testing.T) {
		body := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
		if strings.Contains(body, "Filters applied:") || strings.Contains(body, "Clear filters") || strings.Contains(body, "Clear search") {
			t.Fatalf("an unfiltered list should show no summary or clear link:\n%s", body)
		}
	})
}

// clearLinkHref returns the unescaped href of the body's link labelled text.
// The attribute is HTML-escaped, so a browser reads "+" where the raw bytes
// hold "&#43;".
func clearLinkHref(t *testing.T, body, text string) string {
	t.Helper()
	m := regexp.MustCompile(`<a href="([^"]*)" class="btn-ghost">` + regexp.QuoteMeta(text) + `</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %q link in:\n%s", text, body)
	}
	return html.UnescapeString(m[1])
}

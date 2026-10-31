package admin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// headingSkips reports each heading that sits more than one level below the
// heading before it, and a first heading that is not h1.
func headingSkips(levels []int) []string {
	var skips []string
	prev := 0
	for i, level := range levels {
		if i == 0 && level != 1 {
			skips = append(skips, "first heading is not h1")
		}
		if prev != 0 && level > prev+1 {
			skips = append(skips, fmt.Sprintf("h%d followed by h%d", prev, level))
		}
		prev = level
	}
	return skips
}

// family is the page a shell fixture renders: the 422 variant of a form is
// the same page with errors, so it shares its title.
func family(name string) string {
	if before, _, found := strings.Cut(name, "-"); found {
		return before
	}
	return name
}

func headingLevels(doc *html.Node) []int {
	var levels []int
	walk(doc, func(n *html.Node) {
		if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
			levels = append(levels, int(n.Data[1]-'0'))
		}
	})
	return levels
}

// TestErrorPagesTitleTheirHeading checks that each error page's document title
// is its own heading followed by the admin name, and that no two error pages
// share a title.
func TestErrorPagesTitleTheirHeading(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Widget", 2)
	forbiddenHandler, forbiddenDB := buildProductAdmin(t)
	if _, err := forbiddenDB.Exec(`UPDATE admin_user SET is_staff = $1 WHERE username = $2`, false, "admin"); err != nil {
		t.Fatal(err)
	}
	pages := map[string]*httptest.ResponseRecorder{
		"not found":          doRequest(t, handler, http.MethodGet, crudBasePath+"999999/", nil),
		"method not allowed": doRequest(t, handler, http.MethodPut, crudBasePath, nil),
		"forbidden":          doRequest(t, forbiddenHandler, http.MethodGet, crudBasePath, nil),
	}
	seen := map[string]string{}
	for name, response := range pages {
		doc := parseShell(t, response.Body.String())
		titles := findAll(doc, func(n *html.Node) bool { return n.Data == "title" })
		headings := findAll(doc, func(n *html.Node) bool { return n.Data == "h1" })
		if len(titles) != 1 || len(headings) != 1 {
			t.Fatalf("%s: %d titles and %d h1 elements, want 1 each", name, len(titles), len(headings))
		}
		if got, want := textOf(titles[0]), textOf(headings[0])+" · tanGO Admin"; got != want {
			t.Errorf("%s: title = %q, want %q", name, got, want)
		}
		if other, dup := seen[textOf(titles[0])]; dup {
			t.Errorf("%s: title %q is also the title of %s", name, textOf(titles[0]), other)
		}
		seen[textOf(titles[0])] = name
	}
}

func documentTitle(t *testing.T, body string) string {
	t.Helper()
	titles := findAll(parseShell(t, body), func(n *html.Node) bool { return n.Data == "title" })
	if len(titles) != 1 {
		t.Fatalf("%d title elements, want 1", len(titles))
	}
	return textOf(titles[0])
}

func TestBulkRejectedPageTitleStatesTheOutcome(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	seedProduct(t, sqlDB, "Widget", 2)

	response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkFormOf(0))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if got, want := documentTitle(t, response.Body.String()), "Nothing was deleted · tanGO Admin"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestEmptyModelIndexHasAPageTitle(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)
	cookie := loginAndGetSessionCookie(t, handler, "admin", "correct-password")
	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got, want := documentTitle(t, response.Body.String()), "Models · tanGO Admin"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestEveryAdminPageHasItsDocumentStructure(t *testing.T) {
	titles := map[string]string{}
	for name, body := range shellPages(t) {
		doc := parseShell(t, body)

		root := findAll(doc, func(n *html.Node) bool { return n.Data == "html" })
		if len(root) != 1 || attrValue(root[0], "lang") == "" {
			t.Errorf("%s: <html> has no lang attribute", name)
		}

		title := findAll(doc, func(n *html.Node) bool { return n.Data == "title" })
		if len(title) != 1 || textOf(title[0]) == "" {
			t.Errorf("%s: <title> is missing or empty", name)
		} else if previous, dup := titles[textOf(title[0])]; dup && previous != family(name) {
			t.Errorf("%s: <title> %q is also the title of %s", name, textOf(title[0]), previous)
		} else {
			titles[textOf(title[0])] = family(name)
		}

		if n := findAll(doc, func(n *html.Node) bool { return n.Data == "main" }); len(n) != 1 {
			t.Errorf("%s: %d <main> elements, want 1", name, len(n))
		}

		if n := findAll(doc, func(n *html.Node) bool { return attrValue(n, "role") == "status" }); len(n) != 1 {
			t.Errorf("%s: %d role=\"status\" regions, want 1", name, len(n))
		}

		navs := findAll(doc, func(n *html.Node) bool { return n.Data == "nav" })
		for _, nav := range navs {
			if attrValue(nav, "aria-label") == "" {
				t.Errorf("%s: a <nav> has no aria-label", name)
			}
		}

		for _, skip := range headingSkips(headingLevels(doc)) {
			t.Errorf("%s: heading order: %s", name, skip)
		}
	}
}

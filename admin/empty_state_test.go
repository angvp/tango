package admin_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// emptyStateBlock returns the one empty-state element of a changelist page, so
// assertions about it cannot match markup elsewhere on the page.
func emptyStateBlock(t *testing.T, body string) string {
	t.Helper()
	block := regexp.MustCompile(`(?s)<div class="empty-state">.*?</div>`).FindString(body)
	if block == "" {
		t.Fatalf("the changelist page has no empty-state block")
	}
	return block
}

func TestEmptyStateNamesTheModelAndOffersOnePrimaryAction(t *testing.T) {
	handler, _ := buildMessageBookAdmin(t)
	body := doRequest(t, handler, http.MethodGet, messageBookBasePath, nil).Body.String()
	block := emptyStateBlock(t, body)

	for _, want := range []string{
		"No books yet.",
		"Add the first book to see it here.",
		`<a href="/admin/message_book/new/" class="btn-primary`,
		"&#43; Add book</a>",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("no-rows empty state lacks %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "messageBook") || strings.Contains(block, "No books match") {
		t.Errorf("no-rows empty state carries the wrong copy:\n%s", block)
	}
	if got := strings.Count(block, "<a "); got != 1 {
		t.Errorf("no-rows empty state has %d links, want exactly one primary action", got)
	}
}

func TestEmptyStateWithoutDisplayNounUsesTheHumanizedName(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	block := emptyStateBlock(t, body)
	if !strings.Contains(block, "No crud products yet.") {
		t.Errorf("empty state lacks the humanized name:\n%s", block)
	}
	if strings.Contains(block, "crudProduct") {
		t.Errorf("the raw Go type name reached the empty state:\n%s", block)
	}
}

func TestSearchEmptyStateNamesTheQueryAndClearsTheSearch(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")
	body := doRequest(t, handler, http.MethodGet, messageBookBasePath+"?q=lemon", nil).Body.String()
	block := emptyStateBlock(t, body)

	if !strings.Contains(block, "No books match “lemon”.") {
		t.Errorf("search empty state lacks its headline:\n%s", block)
	}
	if !strings.Contains(block, `<a href="/admin/message_book/" class="btn-secondary`) || !strings.Contains(block, ">Clear search</a>") {
		t.Errorf("search empty state lacks a Clear search action:\n%s", block)
	}
	if strings.Contains(block, "No books yet.") {
		t.Errorf("search empty state claims the model has no rows:\n%s", block)
	}
}

func TestSearchEmptyStateEscapesTheQuery(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")
	body := doRequest(t, handler, http.MethodGet, messageBookBasePath+"?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", nil).Body.String()
	block := emptyStateBlock(t, body)
	if strings.Contains(block, "<script>alert(1)</script>") {
		t.Fatalf("the query reached the empty state unescaped:\n%s", block)
	}
	if !strings.Contains(block, html.EscapeString("<script>alert(1)</script>")) {
		t.Errorf("the escaped query is missing from the empty state:\n%s", block)
	}
}

func TestFilterEmptyStateNamesTheFiltersAndClearsThem(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Draft Only", false, author)

	body := doRequest(t, handler, http.MethodGet, flBookPath+"?filter_published=yes", nil).Body.String()
	block := emptyStateBlock(t, body)
	if !strings.Contains(block, "No fl books match the selected filters.") {
		t.Errorf("filter empty state lacks its headline:\n%s", block)
	}
	if !strings.Contains(block, ">Clear filters</a>") {
		t.Errorf("filter empty state lacks a Clear filters action:\n%s", block)
	}
}

// With a search and a filter both active, the empty state names both and offers
// both clear actions, so neither has to be guessed from the toolbar.
func TestSearchAndFilterEmptyStateOffersBothClears(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Draft Only", false, author)

	body := doRequest(t, handler, http.MethodGet, flBookPath+"?q=lemon&filter_published=yes", nil).Body.String()
	block := emptyStateBlock(t, body)
	if !strings.Contains(block, "No fl books match “lemon” and the selected filters.") {
		t.Errorf("combined empty state lacks its headline:\n%s", block)
	}
	if !strings.Contains(block, ">Clear search</a>") || !strings.Contains(block, ">Clear filters</a>") {
		t.Errorf("combined empty state lacks one of its clear actions:\n%s", block)
	}
}

// The toolbar's clear links are dropped while the empty state shows the same
// actions, so each action appears once on the page.
func TestEmptyStateDoesNotDuplicateTheToolbarClearLinks(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")
	body := doRequest(t, handler, http.MethodGet, messageBookBasePath+"?q=lemon", nil).Body.String()
	if got := strings.Count(body, ">Clear search</a>"); got != 1 {
		t.Errorf("Clear search appears %d times on an empty search page, want 1", got)
	}

	handler, sqlDB = buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	seedBook(t, sqlDB, "Draft Only", false, author)
	body = doRequest(t, handler, http.MethodGet, flBookPath+"?filter_published=yes", nil).Body.String()
	if got := strings.Count(body, ">Clear filters</a>"); got != 1 {
		t.Errorf("Clear filters appears %d times on an empty filter page, want 1", got)
	}
	if !strings.Contains(body, "Filters applied:") {
		t.Errorf("the applied filters are no longer named above an empty list")
	}
}

// A page with rows keeps its toolbar clear link: the duplicate rule applies
// only while the empty state is showing.
func TestSearchWithResultsKeepsTheToolbarClearLink(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")
	body := doRequest(t, handler, http.MethodGet, messageBookBasePath+"?q=Dune", nil).Body.String()
	if got := strings.Count(body, ">Clear search</a>"); got != 1 {
		t.Errorf("Clear search appears %d times on a page with a match, want 1", got)
	}
}

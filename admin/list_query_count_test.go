package admin_test

import (
	"net/http"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/internal/querycount"
)

// changelistStatements loads the post changelist once and returns how many
// statements touched the post table and how many touched the author table.
func changelistStatements(t *testing.T, counter *querycount.Counter, handler http.Handler) (posts, authors int) {
	t.Helper()
	counter.Reset()
	response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("changelist status = %d, want 200", response.Code)
	}
	return counter.Matching("fk_post"), counter.Matching("fk_author")
}

// Known positive for the query counter. Today the changelist resolves each
// labelled foreign-key cell with its own lookup, so three rows send three
// author lookups beside the post count and the post row query. This pins
// today's per-row behaviour; ticket 04 replaces the author figure.
func TestListViewLabelLookupsRunPerRowToday(t *testing.T) {
	counter := querycount.Attach(t)
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	john := seedAuthor(t, sqlDB, "John Roe")
	seedPost(t, sqlDB, "One", jane)
	seedPost(t, sqlDB, "Two", john)
	seedPost(t, sqlDB, "Three", jane)

	posts, authors := changelistStatements(t, counter, handler)
	if posts != 2 {
		t.Fatalf("post statements = %d, want 2 (the count and the row query)", posts)
	}
	if authors != 3 {
		t.Fatalf("author statements = %d, want 3 (one label lookup per row today)", authors)
	}
}

// Known negative beside the positive: the same page over a target with no
// Label records no label lookup. The post statements show the counter saw
// the page, so the zero is a measurement.
func TestListViewWithoutLabelSendsNoLabelLookup(t *testing.T) {
	counter := querycount.Attach(t)
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{})
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	john := seedAuthor(t, sqlDB, "John Roe")
	seedPost(t, sqlDB, "One", jane)
	seedPost(t, sqlDB, "Two", john)
	seedPost(t, sqlDB, "Three", jane)

	posts, authors := changelistStatements(t, counter, handler)
	if posts == 0 {
		t.Fatalf("counter saw no post statements; the zero author count is not a measurement")
	}
	if authors != 0 {
		t.Fatalf("author statements = %d, want 0 without a Label", authors)
	}
}

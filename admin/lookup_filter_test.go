package admin_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func lookupFilterPost() admin.Options {
	post := lookupPostWidget()
	post.ListDisplay = []string{"Title", "AuthorID"}
	post.Filters = []string{"AuthorID"}
	return post
}

func TestLookupFilterListsNoRelatedRows(t *testing.T) {
	handler, sqlDB, counter := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupFilterPost())
	for i := 1; i <= 150; i++ { // more than the select filter's cap of 100
		seedAuthor(t, sqlDB, fmt.Sprintf("Author %03d", i))
	}
	counter.Reset()

	body := getForm(t, handler, "/admin/fk_post/")

	if got := counter.Matching("fk_author"); got != 0 {
		t.Fatalf("the changelist issued %d statements on the authors with no filter value, want 0", got)
	}
	for _, want := range []string{`name="filter_author_id"`, `data-tango-widget="lookup"`, `data-lookup-url="/admin/fk_post/_lookup/AuthorID/"`, `data-lookup-optional`, `/admin/_assets/`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the changelist lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<select") || strings.Contains(body, "too many values") {
		t.Fatalf("the filter is still a select, or says it has too many values:\n%s", body)
	}
}

func TestLookupFilterAppliesAValueWithOneRead(t *testing.T) {
	handler, sqlDB, counter := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupFilterPost())
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	john := seedAuthor(t, sqlDB, "John Roe")
	seedPost(t, sqlDB, "By Jane", jane)
	seedPost(t, sqlDB, "By John", john)
	counter.Reset()

	body := getForm(t, handler, fmt.Sprintf("/admin/fk_post/?filter_author_id=%d", jane))

	if !strings.Contains(body, "By Jane") || strings.Contains(body, "By John") {
		t.Fatalf("the filter did not narrow the list to Jane's posts:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf(`name="filter_author_id" value="%d"`, jane)) || !strings.Contains(body, `data-lookup-label="Jane Doe"`) {
		t.Fatalf("the filter control does not carry the applied key and label:\n%s", body)
	}
	if !strings.Contains(body, "Filters applied:") || !strings.Contains(body, "Author: Jane Doe") {
		t.Fatalf("the applied filter is not named above the table:\n%s", body)
	}
	if got := counter.Matching("fk_author"); got < 1 || got > 2 {
		t.Fatalf("the changelist issued %d statements on the authors, want the one read of the value (plus the label read of the row)", got)
	}
}

func TestLookupFilterIgnoresAStaleOrMalformedValue(t *testing.T) {
	handler, sqlDB, _ := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupFilterPost())
	seedPost(t, sqlDB, "Orphan-free", seedAuthor(t, sqlDB, "Jane Doe"))

	for _, value := range []string{"9999", "abc", "-1"} {
		body := getForm(t, handler, "/admin/fk_post/?filter_author_id="+value)
		if !strings.Contains(body, "Orphan-free") || strings.Contains(body, "Filters applied:") {
			t.Fatalf("filter value %q should be ignored, the page shows:\n%s", value, body)
		}
	}
}

func TestLookupFilterOnAReadOnlyFieldStillSearches(t *testing.T) {
	post := lookupFilterPost()
	post.ReadOnly = []string{"AuthorID"}
	handler, sqlDB, _ := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, post)
	seedAuthor(t, sqlDB, "Jane Doe")

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=Jane", nil))
	if len(answer.Items) != 1 {
		t.Fatalf("the filter's search route answered %+v, want Jane", answer.Items)
	}
}

func TestForeignKeyFilterWithoutALookupIsUnchanged(t *testing.T) {
	post := admin.Options{ListDisplay: []string{"Title", "AuthorID"}, Filters: []string{"AuthorID"}}
	handler, sqlDB, _ := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, post)
	seedAuthor(t, sqlDB, "Jane Doe")

	body := getForm(t, handler, "/admin/fk_post/")

	if !strings.Contains(body, `<select id="filter-author_id" name="filter_author_id"`) || strings.Contains(body, `data-tango-widget="lookup"`) {
		t.Fatalf("a foreign key filter without a lookup is no longer a select:\n%s", body)
	}
}

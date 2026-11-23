package admin_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/internal/querycount"
)

func lookupFormAdmin(t *testing.T, authorOptions admin.Options, postOptions admin.Options) (http.Handler, *sql.DB, *querycount.Counter) {
	t.Helper()
	counter := querycount.Attach(t)
	handler, sqlDB, err := buildLookupAdmin(t, &authorOptions, postOptions)
	if err != nil {
		t.Fatalf("install admin: %v", err)
	}
	return handler, sqlDB, counter
}

func lookupPostWidget() admin.Options {
	return lookupPostOptions(admin.Lookup(admin.LookupOptions{}))
}

func getForm(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	response := doRequest(t, handler, http.MethodGet, path, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, response.Code)
	}
	return response.Body.String()
}

func TestLookupCreateFormLoadsNoRelatedRows(t *testing.T) {
	handler, sqlDB, counter := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupPostWidget())
	for i := 1; i <= 60; i++ {
		seedAuthor(t, sqlDB, fmt.Sprintf("Author %02d", i))
	}
	counter.Reset()

	body := getForm(t, handler, "/admin/fk_post/new/")

	if got := counter.Matching("fk_author"); got != 0 {
		t.Fatalf("the create form issued %d statements on the authors, want 0", got)
	}
	if strings.Contains(body, "<select") || strings.Contains(body, "Author 01") {
		t.Fatalf("the form lists the related rows:\n%s", body)
	}
	if !strings.Contains(body, `name="AuthorID"`) || !strings.Contains(body, `value=""`) {
		t.Fatalf("the form has no empty AuthorID control:\n%s", body)
	}
}

func TestLookupEditFormShowsTheCurrentRowWithOneRead(t *testing.T) {
	handler, sqlDB, counter := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupPostWidget())
	for i := 1; i <= 40; i++ {
		seedAuthor(t, sqlDB, fmt.Sprintf("Author %02d", i))
	}
	chosen := seedAuthor(t, sqlDB, "Zed <b>Last</b>")
	postID := seedPost(t, sqlDB, "Hello", chosen)
	counter.Reset()

	body := getForm(t, handler, fmt.Sprintf("/admin/fk_post/%d/", postID))

	if got := counter.Matching("fk_author"); got != 1 {
		t.Fatalf("the edit form issued %d statements on the authors, want 1 read by key", got)
	}
	if !strings.Contains(body, fmt.Sprintf(`name="AuthorID" value="%d"`, chosen)) {
		t.Fatalf("the control does not hold the stored key %d:\n%s", chosen, body)
	}
	if !strings.Contains(body, "Zed &lt;b&gt;Last&lt;/b&gt;") || strings.Contains(body, "<b>Last</b>") {
		t.Fatalf("the current label is missing or not escaped:\n%s", body)
	}
	if strings.Contains(body, "Author 01") {
		t.Fatalf("the form lists rows other than the current one")
	}
}

func TestLookupUnsetKeyRendersAnEmptyControl(t *testing.T) {
	handler, sqlDB, _ := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupPostWidget())
	postID := insertReturningID(t, sqlDB, `INSERT INTO fk_post (title, author_id) VALUES ($1, NULL) RETURNING id`, "No author")

	body := getForm(t, handler, fmt.Sprintf("/admin/fk_post/%d/", postID))

	if !strings.Contains(body, `name="AuthorID" value=""`) {
		t.Fatalf("an unset key does not render empty:\n%s", body)
	}
}

func TestLookupFormKeepsTheSubmittedKeyAfterAValidationFailure(t *testing.T) {
	handler, _, _ := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, lookupPostWidget())

	missing := doRequest(t, handler, http.MethodPost, "/admin/fk_post/new/", url.Values{"Title": {"x"}, "AuthorID": {"9999"}})
	body := missing.Body.String()
	if strings.Contains(body, "<select") || !strings.Contains(body, `name="AuthorID" value="9999"`) || !strings.Contains(body, "The item you chose no longer exists") {
		t.Fatalf("a missing related row does not keep its key and report the existing error:\n%s", body)
	}
}

func TestLookupReadOnlyFieldShowsLabelAndKey(t *testing.T) {
	post := lookupPostWidget()
	post.ReadOnly = []string{"AuthorID"}
	handler, sqlDB, _ := lookupFormAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, post)
	author := seedAuthor(t, sqlDB, "Jane Doe")
	postID := seedPost(t, sqlDB, "Hello", author)

	body := getForm(t, handler, fmt.Sprintf("/admin/fk_post/%d/", postID))

	if !strings.Contains(body, fmt.Sprintf(`value="Jane Doe (%d)"`, author)) {
		t.Fatalf("a read-only lookup does not show label and key:\n%s", body)
	}
}

func TestLookupReadOnlyFieldWithoutALabelShowsTheKey(t *testing.T) {
	post := lookupPostWidget()
	post.ReadOnly = []string{"AuthorID"}
	handler, sqlDB, _ := lookupFormAdmin(t, admin.Options{Search: []string{"Name"}}, post)
	author := seedAuthor(t, sqlDB, "Jane Doe")
	postID := seedPost(t, sqlDB, "Hello", author)

	body := getForm(t, handler, fmt.Sprintf("/admin/fk_post/%d/", postID))

	if !strings.Contains(body, fmt.Sprintf(`id="field-AuthorID" type="text" value="%d" readonly`, author)) {
		t.Fatalf("a read-only lookup without a label does not show the key:\n%s", body)
	}
}

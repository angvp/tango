package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/internal/querycount"
)

type fkTag struct {
	ID   int64 `tango:"pk"`
	Name string
}

type fkArticle struct {
	ID       int64 `tango:"pk"`
	Title    string
	AuthorID int64 `tango:"fk=fkAuthor"`
	TagID    int64 `tango:"fk=fkTag"`
}

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

// buildArticleAdmin registers fkArticle, which has two foreign keys, beside
// the author and tag targets, each with its own admin options.
func buildArticleAdmin(t *testing.T, authorOptions, tagOptions, articleOptions admin.Options) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	for _, m := range []any{fkAuthor{}, fkTag{}, fkArticle{}} {
		if err := registry.Models().Register(m); err != nil {
			t.Fatalf("register %T: %v", m, err)
		}
	}
	if err := registry.Admin().Register(fkAuthor{}, authorOptions); err != nil {
		t.Fatalf("register admin fkAuthor: %v", err)
	}
	if err := registry.Admin().Register(fkTag{}, tagOptions); err != nil {
		t.Fatalf("register admin fkTag: %v", err)
	}
	if err := registry.Admin().Register(fkArticle{}, articleOptions); err != nil {
		t.Fatalf("register admin fkArticle: %v", err)
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

// articleListOptions shows the title, then the author and tag labels.
var articleListOptions = admin.Options{ListDisplay: []string{"Title", "AuthorID", "TagID"}}

func seedTag(t *testing.T, sqlDB *sql.DB, name string) int64 {
	t.Helper()
	return insertReturningID(t, sqlDB, `INSERT INTO fk_tag (name) VALUES ($1) RETURNING id`, name)
}

func seedArticle(t *testing.T, sqlDB *sql.DB, title string, authorID, tagID int64) {
	t.Helper()
	if _, err := sqlDB.Exec(`INSERT INTO fk_article (title, author_id, tag_id) VALUES ($1, $2, $3)`, title, authorID, tagID); err != nil {
		t.Fatalf("seed article: %v", err)
	}
}

// Three rows over two authors, so a per-row lookup would send three author
// statements and a per-column read sends one.
func TestListViewLoadsEachLabelColumnOnce(t *testing.T) {
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
	if authors != 1 {
		t.Fatalf("author statements = %d, want 1 (one label read for the column)", authors)
	}

	// Control: the same counter sees one statement per row when the rows are
	// read one at a time, so the single statement above is a measurement.
	counter.Reset()
	for _, id := range []int64{jane, john, jane} {
		var name string
		if err := sqlDB.QueryRowContext(context.Background(), `SELECT name FROM fk_author WHERE id = $1`, id).Scan(&name); err != nil {
			t.Fatalf("per-row control lookup: %v", err)
		}
	}
	if got := counter.Matching("fk_author"); got != 3 {
		t.Fatalf("per-row control author statements = %d, want 3", got)
	}
}

// Two labelled foreign-key columns send one label statement each.
func TestListViewSendsOneLabelStatementPerLabelledColumn(t *testing.T) {
	counter := querycount.Attach(t)
	handler, sqlDB := buildArticleAdmin(t, admin.Options{Label: "Name"}, admin.Options{Label: "Name"}, articleListOptions)
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	john := seedAuthor(t, sqlDB, "John Roe")
	news := seedTag(t, sqlDB, "News")
	food := seedTag(t, sqlDB, "Food")
	seedArticle(t, sqlDB, "One", jane, news)
	seedArticle(t, sqlDB, "Two", john, food)
	seedArticle(t, sqlDB, "Three", jane, food)
	counter.Reset()

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_article/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("changelist status = %d, want 200", response.Code)
	}
	if got := counter.Matching("fk_article"); got != 2 {
		t.Fatalf("article statements = %d, want 2 (the count and the row query)", got)
	}
	if got := counter.Matching("fk_author"); got != 1 {
		t.Fatalf("author statements = %d, want 1", got)
	}
	if got := counter.Matching("fk_tag"); got != 1 {
		t.Fatalf("tag statements = %d, want 1", got)
	}
	body := response.Body.String()
	for _, label := range []string{"Jane Doe", "John Roe", "News", "Food"} {
		if !strings.Contains(body, label) {
			t.Fatalf("changelist does not show label %q:\n%s", label, body)
		}
	}
}

// A column whose target has no Label sends no label statement, while the
// labelled column beside it still sends its one.
func TestListViewSendsNoLabelStatementForUnlabelledColumn(t *testing.T) {
	counter := querycount.Attach(t)
	handler, sqlDB := buildArticleAdmin(t, admin.Options{Label: "Name"}, admin.Options{}, articleListOptions)
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	news := seedTag(t, sqlDB, "News")
	seedArticle(t, sqlDB, "One", jane, news)
	seedArticle(t, sqlDB, "Two", jane, news)
	counter.Reset()

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_article/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("changelist status = %d, want 200", response.Code)
	}
	if got := counter.Matching("fk_article"); got == 0 {
		t.Fatalf("counter saw no article statements; the zero tag count is not a measurement")
	}
	if got := counter.Matching("fk_author"); got != 1 {
		t.Fatalf("author statements = %d, want 1", got)
	}
	if got := counter.Matching("fk_tag"); got != 0 {
		t.Fatalf("tag statements = %d, want 0 for an unlabelled target", got)
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

// A failed label read does not fail the page: the column shows raw keys and
// the error is logged.
func TestListViewRendersRawKeysWhenLabelReadFails(t *testing.T) {
	counter := querycount.Attach(t)
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	seedPost(t, sqlDB, "One", jane)

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	counter.FailMatching("fk_author", errors.New("injected label read failure"))
	response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("changelist status = %d, want 200 when the label read fails", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "Jane Doe") {
		t.Fatalf("changelist shows a label although the read failed:\n%s", body)
	}
	if !strings.Contains(body, ">"+itoa(jane)+"<") {
		t.Fatalf("changelist does not fall back to the raw key %d:\n%s", jane, body)
	}
	if !strings.Contains(logs.String(), "injected label read failure") {
		t.Fatalf("label read failure was not logged; log:\n%s", logs.String())
	}
}

// A labelled foreign key in the first column is the row's identity: the
// bulk-delete checkbox and the row actions carry its label, and the identity
// costs no lookup beyond the column's one.
func TestListViewIdentityCarriesTheLabelWithoutPerRowLookup(t *testing.T) {
	counter := querycount.Attach(t)
	identityOptions := admin.Options{ListDisplay: []string{"AuthorID", "Title"}, BulkDelete: true}
	handler, sqlDB := buildArticleAdmin(t, admin.Options{Label: "Name"}, admin.Options{}, identityOptions)
	jane := seedAuthor(t, sqlDB, "Jane Doe")
	john := seedAuthor(t, sqlDB, "John Roe")
	news := seedTag(t, sqlDB, "News")
	seedArticle(t, sqlDB, "One", jane, news)
	seedArticle(t, sqlDB, "Two", john, news)
	seedArticle(t, sqlDB, "Three", jane, news)
	counter.Reset()

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_article/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("changelist status = %d, want 200", response.Code)
	}
	if got := counter.Matching("fk_author"); got != 1 {
		t.Fatalf("author statements = %d, want 1", got)
	}
	body := response.Body.String()
	for _, identity := range []string{`data-bulk-identity="Jane Doe"`, `data-bulk-identity="John Roe"`} {
		if !strings.Contains(body, identity) {
			t.Fatalf("changelist does not carry %s:\n%s", identity, body)
		}
	}
}

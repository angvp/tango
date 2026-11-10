package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/model"

	"board/apps/posts"
	"board/migrations"
	"board/project"
)

// newCountingApp is newTestApp over the counting SQLite database, so the
// statements a request sends are recorded on the returned counter.
func newCountingApp(t *testing.T) (*testApp, *statementCounter) {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // keep test output quiet
	sqlDB, counter := openCountingDB(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, db.SQLite, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(sqlDB, db.SQLite)
	t.Setenv("BOARD_JWT_SECRET", strings.Repeat("s", 32))

	registry, err := tango.BuildRegistry(project.Config(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	registry.SetStore(store)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{handler: handler, store: store, registry: registry}, counter
}

// seedBylines makes two accounts, three posts (two owned, one with no owner)
// and two comments on the first post. The comments come back in ID order.
func seedBylines(t *testing.T, app *testApp) (adaID, graceID int64) {
	t.Helper()
	ctx := t.Context()
	app.signUp(t, "ada@example.com", "correct horse")
	app.signUp(t, "grace@example.com", "correct horse")
	adaID = accountID(t, app, "ada@example.com")
	graceID = accountID(t, app, "grace@example.com")

	postMeta := mustMeta(t, app, "Post")
	commentMeta := mustMeta(t, app, "Comment")
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	seeds := []posts.Post{
		{Title: "First", Body: "a", AccountID: adaID, CreatedAt: base},
		{Title: "Second", Body: "b", AccountID: graceID, CreatedAt: base.Add(time.Minute)},
		{Title: "Orphan", Body: "c", CreatedAt: base.Add(2 * time.Minute)},
	}
	for i := range seeds {
		if err := app.store.Create(ctx, postMeta, &seeds[i]); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"one", "two"} {
		comment := posts.Comment{PostID: seeds[0].ID, Author: "ada", Body: body, CreatedAt: base}
		if err := app.store.Create(ctx, commentMeta, &comment); err != nil {
			t.Fatal(err)
		}
	}
	return adaID, graceID
}

func accountID(t *testing.T, app *testApp, email string) int64 {
	t.Helper()
	var account accounts.Account
	query := db.Query{Where: []db.Condition{{Field: "Email", Op: db.OpEq, Value: email}}}
	if err := app.store.First(t.Context(), mustMeta(t, app, "Account"), query, &account); err != nil {
		t.Fatal(err)
	}
	return account.ID
}

func mustMeta(t *testing.T, app *testApp, name string) model.ModelMeta {
	t.Helper()
	meta, ok := app.registry.Models().Get(name)
	if !ok {
		t.Fatalf("model %s is not registered", name)
	}
	return meta
}

// summaryJSON is the list response's shape. Byline is the only owner field.
type summaryJSON struct {
	Title  string `json:"Title"`
	Byline string `json:"Byline"`
}

// recentJSON is the recent read's shape: the account's public fields and the
// post's comments.
type recentJSON struct {
	Title   string `json:"title"`
	Account *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
	Comments []struct {
		ID   int64  `json:"ID"`
		Body string `json:"Body"`
	} `json:"comments"`
}

func getBody(t *testing.T, app *testApp, path string) string {
	t.Helper()
	rec := app.do("GET", path, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, rec.Code)
	}
	return rec.Body.String()
}

func TestHomeShowsEachPostsByline(t *testing.T) {
	app, _ := newCountingApp(t)
	seedBylines(t, app)

	body := getBody(t, app, "/")
	for _, want := range []string{"by ada ·", "by grace ·"} {
		if n := strings.Count(body, want); n != 1 {
			t.Errorf("home page has %d %q bylines, want 1", n, want)
		}
	}
	if strings.Contains(body, "by  ·") {
		t.Errorf("orphan post rendered an empty byline")
	}
}

func TestPostListCarriesBylinesAndNoPrivateFields(t *testing.T) {
	app, _ := newCountingApp(t)
	seedBylines(t, app)

	body := getBody(t, app, "/posts/")
	var listed []summaryJSON
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"First": "ada", "Second": "grace", "Orphan": ""}
	if len(listed) != len(want) {
		t.Fatalf("listed %d posts, want %d", len(listed), len(want))
	}
	for _, p := range listed {
		if p.Byline != want[p.Title] {
			t.Errorf("post %q byline %q, want %q", p.Title, p.Byline, want[p.Title])
		}
	}
	for _, leaked := range []string{"PasswordHash", "@example.com", "AccountID"} {
		if strings.Contains(body, leaked) {
			t.Errorf("list response exposes %q: %s", leaked, body)
		}
	}
}

// TestPostListIsTwoStatements: the raw summary query, then one statement for
// the bylines of the whole page. The known-positive control issues one lookup
// per post on the same counter and must register one statement per post.
func TestPostListIsTwoStatements(t *testing.T) {
	app, counter := newCountingApp(t)
	seedBylines(t, app)
	accountMeta := mustMeta(t, app, "Account")

	before := counter.Count()
	getBody(t, app, "/posts/")
	if got := counter.Count() - before; got != 2 {
		t.Fatalf("list counted %d statements, want 2 (summaries, then bylines)", got)
	}

	const postCount = 3
	adaID := accountID(t, app, "ada@example.com")
	before = counter.Count()
	for range postCount {
		var account accounts.Account
		if err := app.store.Get(t.Context(), accountMeta, adaID, &account); err != nil {
			t.Fatal(err)
		}
	}
	if got := counter.Count() - before; got != postCount {
		t.Fatalf("per-post control counted %d statements, want %d", got, postCount)
	}
}

func TestRecentPostsCarryAccountAndCommentsWithoutPrivateFields(t *testing.T) {
	app, _ := newCountingApp(t)
	adaID, _ := seedBylines(t, app)

	body := getBody(t, app, "/posts/recent/")
	var recent []recentJSON
	if err := json.Unmarshal([]byte(body), &recent); err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]recentJSON{}
	for _, p := range recent {
		byTitle[p.Title] = p
	}
	if len(byTitle) != 3 {
		t.Fatalf("recent returned %d posts, want 3: %s", len(byTitle), body)
	}

	first := byTitle["First"]
	if first.Account == nil || first.Account.ID != adaID || first.Account.Name != "ada" {
		t.Errorf("First account = %+v, want ada (ID %d)", first.Account, adaID)
	}
	if len(first.Comments) != 2 || first.Comments[0].ID >= first.Comments[1].ID {
		t.Errorf("First comments %+v, want two in ascending ID order", first.Comments)
	}

	if !strings.Contains(body, `"comments":[]`) {
		t.Errorf("a post with no comments must carry an empty comments array: %s", body)
	}
	if byTitle["Orphan"].Account != nil {
		t.Errorf("orphan post has an account: %+v", byTitle["Orphan"].Account)
	}
	for _, leaked := range []string{"PasswordHash", "@example.com", "email"} {
		if strings.Contains(body, leaked) {
			t.Errorf("recent response exposes %q: %s", leaked, body)
		}
	}
}

// TestRecentPostsIsThreeStatements counts the page's statements, then runs a
// per-post comment lookup on the same counter as the known-positive control.
func TestRecentPostsIsThreeStatements(t *testing.T) {
	app, counter := newCountingApp(t)
	seedBylines(t, app)
	commentMeta := mustMeta(t, app, "Comment")
	postMeta := mustMeta(t, app, "Post")

	before := counter.Count()
	getBody(t, app, "/posts/recent/")
	if got := counter.Count() - before; got != 3 {
		t.Fatalf("recent page counted %d statements, want 3 (posts, accounts, comments)", got)
	}

	var listed []posts.Post
	if err := app.store.List(t.Context(), postMeta, db.Query{}, &listed); err != nil {
		t.Fatal(err)
	}
	before = counter.Count()
	for _, p := range listed {
		var comments []posts.Comment
		query := db.Query{Where: []db.Condition{{Field: "PostID", Op: db.OpEq, Value: p.ID}}}
		if err := app.store.List(t.Context(), commentMeta, query, &comments); err != nil {
			t.Fatal(err)
		}
	}
	if got := counter.Count() - before; got != int64(len(listed)) {
		t.Fatalf("per-post control counted %d statements, want %d", got, len(listed))
	}
}

func TestRecentPostsIsBounded(t *testing.T) {
	app, _ := newCountingApp(t)
	postMeta := mustMeta(t, app, "Post")
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i := range 25 {
		p := posts.Post{Title: "P", Body: "b", CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := app.store.Create(t.Context(), postMeta, &p); err != nil {
			t.Fatal(err)
		}
	}
	var recent []recentJSON
	if err := json.Unmarshal([]byte(getBody(t, app, "/posts/recent/")), &recent); err != nil {
		t.Fatal(err)
	}
	if len(recent) != 20 {
		t.Fatalf("recent returned %d posts for 25 stored, want a page of 20", len(recent))
	}
}

// TestBylineHasNoNewMigration: relation fields are not columns.
func TestBylineHasNoNewMigration(t *testing.T) {
	app, _ := newCountingApp(t)
	state, err := migration.Replay(migrations.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := migration.Diff(app.registry.Models().All(), state)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("relation fields produced %d pending migrations, want 0", len(pending))
	}
	control, err := migration.Diff(app.registry.Models().All(), migration.SchemaState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(control) == 0 {
		t.Fatal("diff from an empty state produced no migration; the zero above proves nothing")
	}
}

// TestCreateIgnoresClientSentAccount: a client-sent account object is never
// written, and the owner is still the token's account.
func TestCreateIgnoresClientSentAccount(t *testing.T) {
	app, _ := newCountingApp(t)
	app.signUp(t, "ada@example.com", "correct horse")
	adaID := accountID(t, app, "ada@example.com")
	token := app.token(t, "ada@example.com", "correct horse")

	body := `{"title":"Injected","Account":{"ID":999,"Email":"mallory@example.com"}}`
	rec := app.do("POST", "/posts/", body, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body)
	}
	var created struct{ AccountID int64 }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.AccountID != adaID {
		t.Errorf("AccountID = %d, want %d from the token", created.AccountID, adaID)
	}
	var mallory accounts.Account
	err := app.store.Get(t.Context(), mustMeta(t, app, "Account"), int64(999), &mallory)
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("client-sent account row was written or lookup failed differently: %v", err)
	}
}

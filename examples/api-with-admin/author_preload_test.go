package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/observability"

	"api-with-admin/apps/authors"
	"api-with-admin/apps/posts"
	"api-with-admin/migrations"
)

// newCountingServer is newTestServer over the counting SQLite database, and
// returns the counter the handler's statements are recorded on.
func newCountingServer(t *testing.T) (http.Handler, *db.Store, *tango.Registry, *statementCounter) {
	t.Helper()
	sqlDB, counter := openCountingDB(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, db.SQLite, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(sqlDB, db.SQLite)
	registry, err := tango.BuildRegistry(appConfig(store, sqlDB, db.SQLite, observability.NewWindow()))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, store, registry, counter
}

// seedAuthorsAndPosts creates two authors and three posts: two by Ada and
// Grace, and one with no author (AuthorID 0 is stored as NULL).
func seedAuthorsAndPosts(t *testing.T, store *db.Store, registry *tango.Registry) (ada, grace authors.Author) {
	t.Helper()
	ctx := t.Context()
	authorMeta, _ := registry.Models().Get("Author")
	postMeta, _ := registry.Models().Get("Post")
	ada = authors.Author{Name: "Ada", Email: "ada@example.com"}
	grace = authors.Author{Name: "Grace", Email: "grace@example.com"}
	if err := store.Create(ctx, authorMeta, &ada); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, authorMeta, &grace); err != nil {
		t.Fatal(err)
	}
	for _, post := range []posts.Post{
		{Title: "First", Body: "a", AuthorID: ada.ID},
		{Title: "Second", Body: "b", AuthorID: grace.ID},
		{Title: "Orphan", Body: "c"},
	} {
		if err := store.Create(ctx, postMeta, &post); err != nil {
			t.Fatal(err)
		}
	}
	return ada, grace
}

// listedPost is the list response's shape, with the author kept raw so a
// test can tell an absent member from a null one.
type listedPost struct {
	ID     int64           `json:"ID"`
	Title  string          `json:"Title"`
	Author json.RawMessage `json:"author"`
}

func getJSON[T any](t *testing.T, handler http.Handler, path string) T {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, rec.Code)
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET %s: decoding: %v\n%s", path, err, rec.Body.String())
	}
	return out
}

func TestPostListEmbedsEachPostsAuthor(t *testing.T) {
	handler, store, registry, _ := newCountingServer(t)
	ada, grace := seedAuthorsAndPosts(t, store, registry)

	listed := getJSON[[]listedPost](t, handler, "/posts/")
	if len(listed) != 3 {
		t.Fatalf("listed %d posts, want 3", len(listed))
	}
	wantByTitle := map[string]int64{"First": ada.ID, "Second": grace.ID}
	for _, p := range listed {
		wantID, hasAuthor := wantByTitle[p.Title]
		if !hasAuthor {
			if len(p.Author) != 0 {
				t.Errorf("post %q with no author has an author member: %s", p.Title, p.Author)
			}
			continue
		}
		var author authors.Author
		if err := json.Unmarshal(p.Author, &author); err != nil {
			t.Fatalf("post %q: author member %s: %v", p.Title, p.Author, err)
		}
		if author.ID != wantID {
			t.Errorf("post %q: author ID %d, want %d", p.Title, author.ID, wantID)
		}
	}
}

// seedPostsOfSize creates two authors and size posts that alternate between
// them and no author, so every size has posts with and without an author.
func seedPostsOfSize(t *testing.T, store *db.Store, registry *tango.Registry, size int) authors.Author {
	t.Helper()
	ctx := t.Context()
	authorMeta, _ := registry.Models().Get("Author")
	postMeta, _ := registry.Models().Get("Post")
	ada := authors.Author{Name: "Ada", Email: "ada@example.com"}
	grace := authors.Author{Name: "Grace", Email: "grace@example.com"}
	if err := store.Create(ctx, authorMeta, &ada); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, authorMeta, &grace); err != nil {
		t.Fatal(err)
	}
	owners := []int64{ada.ID, grace.ID, 0}
	for i := range size {
		post := posts.Post{Title: fmt.Sprintf("post-%d", i), Body: "b", AuthorID: owners[i%len(owners)]}
		if err := store.Create(ctx, postMeta, &post); err != nil {
			t.Fatal(err)
		}
	}
	return ada
}

// TestPostListIsTwoStatementsAtEverySize is the count claim at several list
// sizes. At each size the list must send two statements, rows then authors,
// and a known-positive control on the same counter must count one statement
// per post for the same rows read one at a time.
func TestPostListIsTwoStatementsAtEverySize(t *testing.T) {
	for _, size := range []int{1, 3, 50} {
		t.Run(fmt.Sprintf("%d posts", size), func(t *testing.T) {
			handler, store, registry, counter := newCountingServer(t)
			ada := seedPostsOfSize(t, store, registry, size)
			authorMeta, _ := registry.Models().Get("Author")

			before := counter.Count()
			listed := getJSON[[]listedPost](t, handler, "/posts/")
			if len(listed) != size {
				t.Fatalf("listed %d posts, want %d", len(listed), size)
			}
			if got := counter.Count() - before; got != 2 {
				t.Fatalf("list counted %d statements, want 2 (rows, then authors)", got)
			}

			before = counter.Count()
			for range size {
				var author authors.Author
				if err := store.Get(t.Context(), authorMeta, ada.ID, &author); err != nil {
					t.Fatal(err)
				}
			}
			if got := counter.Count() - before; got != int64(size) {
				t.Fatalf("per-post control counted %d statements, want %d", got, size)
			}
		})
	}
}

func TestSinglePostReadIsUnchanged(t *testing.T) {
	handler, store, registry, _ := newCountingServer(t)
	ada, _ := seedAuthorsAndPosts(t, store, registry)
	postMeta, _ := registry.Models().Get("Post")
	var first posts.Post
	if err := store.First(t.Context(), postMeta, db.Query{Where: []db.Condition{{Field: "AuthorID", Op: db.OpEq, Value: ada.ID}}}, &first); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/posts/"+strconv.FormatInt(first.ID, 10)+"/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ID", "Title", "Body", "AuthorID", "CreatedAt"} {
		if _, ok := keys[key]; !ok {
			t.Errorf("single post response lacks %s: %s", key, rec.Body.String())
		}
	}
	if _, ok := keys["author"]; ok {
		t.Errorf("single post response grew an author member: %s", rec.Body.String())
	}
}

// TestCreateIgnoresClientSentAuthor sends an author object in the create body.
// The post is created with the AuthorID it names, the object is ignored, and
// no author row is written from it.
func TestCreateIgnoresClientSentAuthor(t *testing.T) {
	handler, store, registry, _ := newCountingServer(t)
	ada, _ := seedAuthorsAndPosts(t, store, registry)
	authorMeta, _ := registry.Models().Get("Author")

	body := `{"Title":"Injected","Body":"x","AuthorID":` + strconv.FormatInt(ada.ID, 10) + `,"author":{"ID":999,"Name":"Mallory","Email":"m@example.com"}}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/posts/", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Mallory") {
		t.Errorf("create response echoes the client-sent author: %s", rec.Body.String())
	}

	var created posts.Post
	if err := store.First(t.Context(), mustMeta(t, registry, "Post"), db.Query{Where: []db.Condition{{Field: "Title", Op: db.OpEq, Value: "Injected"}}}, &created); err != nil {
		t.Fatal(err)
	}
	if created.AuthorID != ada.ID {
		t.Errorf("stored AuthorID %d, want %d", created.AuthorID, ada.ID)
	}
	var mallory authors.Author
	err := store.Get(t.Context(), authorMeta, int64(999), &mallory)
	if err == nil {
		t.Errorf("client-sent author row was written: %+v", mallory)
	}
}

// TestPostHasNoNewMigration replays the example's migrations and diffs them
// against the registered models. Relation fields are not columns, so the diff
// is empty. The same diff from an empty state is non-empty, which shows the
// comparison sees the tables and the zero result is not a silent no-op.
func TestPostHasNoNewMigration(t *testing.T) {
	_, _, registry, _ := newCountingServer(t)
	state, err := migration.Replay(migrations.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := migration.Diff(registry.Models().All(), state)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("relation fields produced %d pending migrations, want 0", len(pending))
	}

	control, err := migration.Diff(registry.Models().All(), migration.SchemaState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(control) == 0 {
		t.Fatal("diff from an empty state produced no migration; the zero above proves nothing")
	}
}

func mustMeta(t *testing.T, registry *tango.Registry, name string) model.ModelMeta {
	t.Helper()
	meta, ok := registry.Models().Get(name)
	if !ok {
		t.Fatalf("model %s is not registered", name)
	}
	return meta
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"

	"api-with-admin/apps/authors"
	"api-with-admin/migrations"
)

// newTestServer is the real application, built from the same appConfig as
// main, on a fresh, fully migrated database.
func newTestServer(t *testing.T) (http.Handler, *db.Store, *tango.Registry) {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(sqlDB, dialect)
	registry, err := tango.BuildRegistry(appConfig(store))
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
	return handler, store, registry
}

// TestAPostCreatedThroughTheAPIBelongsToItsAuthor is the example's point:
// a JSON API and the admin share one set of models, linked by a foreign
// key.
func TestAPostCreatedThroughTheAPIBelongsToItsAuthor(t *testing.T) {
	handler, store, registry := newTestServer(t)
	author := authors.Author{Name: "Ada", Email: "ada@example.com"}
	meta, ok := registry.Models().Get("Author")
	if !ok {
		t.Fatal("Author isn't registered")
	}
	if err := store.Create(t.Context(), meta, &author); err != nil {
		t.Fatal(err)
	}

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/posts/",
		strings.NewReader(`{"Title":"Hello","Body":"First post","AuthorID":`+strconv.FormatInt(author.ID, 10)+`}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("POST /posts/ = %d %s", created.Code, created.Body.String())
	}

	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/posts/?author_id="+strconv.FormatInt(author.ID, 10), nil))
	var posts []struct{ Title string }
	if err := json.Unmarshal(listed.Body.Bytes(), &posts); err != nil || len(posts) != 1 || posts[0].Title != "Hello" {
		t.Fatalf("GET /posts/?author_id= = %d %s", listed.Code, listed.Body.String())
	}

	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if admin.Code != http.StatusFound || !strings.HasPrefix(admin.Header().Get("Location"), "/admin/login/") {
		t.Fatalf("GET /admin/ = %d to %q, want the admin's login redirect", admin.Code, admin.Header().Get("Location"))
	}
}

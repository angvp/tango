package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
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
// key: a post the API creates for an author is listed by author, and
// shows in the admin.
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

	// The admin, logged in, lists the post the API created.
	if err := admin.CreateAccount(t.Context(), store, "editor", "correct-password"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	loginPage := fetch(t, browser, server.URL+"/admin/login/")
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(loginPage)
	if csrf == nil {
		t.Fatalf("the admin login page has no CSRF token:\n%s", loginPage)
	}
	loggedIn, err := browser.PostForm(server.URL+"/admin/login/", url.Values{
		"username": {"editor"}, "password": {"correct-password"}, "csrf_token": {csrf[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	loggedIn.Body.Close()
	if loggedIn.StatusCode != http.StatusFound {
		t.Fatalf("admin login = %d", loggedIn.StatusCode)
	}
	if list := fetch(t, browser, server.URL+"/admin/post/"); !strings.Contains(list, "Hello") {
		t.Fatalf("the admin's post list doesn't show the API's post:\n%s", list)
	}
}

// fetch GETs target with browser and returns its body.
func fetch(t *testing.T, browser *http.Client, target string) string {
	t.Helper()
	response, err := browser.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

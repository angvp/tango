package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"api-with-admin/apps/authors"
	"api-with-admin/apps/posts"
	"github.com/angvp/tango/admin"
)

// lookupStatements is what one admin session spends, in statements, on the
// post form, the form of a post whose author is far down the table, and one
// author search. The numbers include the session lookup each request makes;
// only their comparison between a small and a large table means anything.
type lookupStatements struct {
	newForm, editForm, filteredList, search int64
	editBody, searchBody                    string
}

// measureLookup seeds count authors, one post by the last of them, logs in to
// the admin and measures the post pages that reach the authors.
func measureLookup(t *testing.T, count int) lookupStatements {
	t.Helper()
	handler, store, registry, counter := newCountingServer(t)
	ctx := t.Context()
	authorMeta, _ := registry.Models().Get("Author")
	postMeta, _ := registry.Models().Get("Post")
	var last authors.Author
	for i := 1; i <= count; i++ {
		last = authors.Author{Name: fmt.Sprintf("Author %04d", i), Email: fmt.Sprintf("author%04d@example.com", i)}
		if err := store.Create(ctx, authorMeta, &last); err != nil {
			t.Fatal(err)
		}
	}
	post := posts.Post{Title: "Late author", Body: "b", AuthorID: last.ID}
	if err := store.Create(ctx, postMeta, &post); err != nil {
		t.Fatal(err)
	}
	if err := admin.CreateAccount(ctx, store, "editor", "correct-password"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(fetch(t, browser, server.URL+"/admin/login/"))
	if csrf == nil {
		t.Fatal("the admin login page has no CSRF token")
	}
	loggedIn, err := browser.PostForm(server.URL+"/admin/login/", url.Values{
		"username": {"editor"}, "password": {"correct-password"}, "csrf_token": {csrf[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	loggedIn.Body.Close()

	measure := func(path string) (int64, string) {
		before := counter.Count()
		body := fetch(t, browser, server.URL+path)
		return counter.Count() - before, body
	}
	var got lookupStatements
	var newBody, listBody string
	got.newForm, newBody = measure("/admin/post/new/")
	got.editForm, got.editBody = measure(fmt.Sprintf("/admin/post/%d/", post.ID))
	got.filteredList, listBody = measure(fmt.Sprintf("/admin/post/?filter_author_id=%d", last.ID))
	got.search, got.searchBody = measure("/admin/post/_lookup/AuthorID/?q=" + url.QueryEscape(last.Name))
	if strings.Contains(newBody, "<select") || strings.Contains(got.editBody, "<select") || strings.Contains(listBody, "<select name=\"filter_author_id\"") {
		t.Fatalf("a post page still lists the authors in a select")
	}
	return got
}

// The cost of the post pages must not grow with the authors table: the
// numbers for a handful of authors and for thousands are the same.
func TestPostPagesCostTheSameWhateverTheNumberOfAuthors(t *testing.T) {
	small := measureLookup(t, 5)
	large := measureLookup(t, 3000)

	if small.newForm != large.newForm || small.editForm != large.editForm || small.filteredList != large.filteredList || small.search != large.search {
		t.Fatalf("statements grow with the authors table: 5 authors %+v, 3000 authors %+v", small, large)
	}
	// The edit form reads the post and the one author it names.
	if large.editForm != large.newForm+2 {
		t.Fatalf("the edit form issued %d statements and the new form %d, want two more: the post and its author", large.editForm, large.newForm)
	}
}

func TestPostFormShowsTheAuthorFarDownTheTable(t *testing.T) {
	large := measureLookup(t, 3000)

	if !strings.Contains(large.editBody, `data-lookup-label="Author 3000"`) {
		t.Fatalf("the edit form does not carry the stored author's name:\n%s", large.editBody)
	}
	if strings.Contains(large.editBody, "Author 0001") {
		t.Fatalf("the edit form lists authors other than the stored one")
	}
}

func TestAuthorSearchReturnsOnlyKeyAndName(t *testing.T) {
	large := measureLookup(t, 3000)

	var answer struct {
		Items []map[string]any `json:"items"`
		More  bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(large.searchBody), &answer); err != nil {
		t.Fatalf("decode %q: %v", large.searchBody, err)
	}
	if len(answer.Items) != 1 || answer.Items[0]["text"] != "Author 3000" || len(answer.Items[0]) != 2 || answer.More {
		t.Fatalf("search answered %s, want one author with a value and a text", large.searchBody)
	}
	if strings.Contains(large.searchBody, "@example.com") {
		t.Fatalf("the search leaks the email column: %s", large.searchBody)
	}
}

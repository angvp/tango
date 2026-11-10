package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

var (
	adminHeaderCell = regexp.MustCompile(`(?s)<th[^>]*>(.*?)</th>`)
	adminFieldID    = regexp.MustCompile(`id="field-([^"]+)"`)
	adminTag        = regexp.MustCompile(`<[^>]+>`)
)

// adminPostPages logs in to the admin with a seeded Post and returns the
// changelist and the add form bodies.
func adminPostPages(t *testing.T) (list, form string) {
	t.Helper()
	handler, store, registry, _ := newCountingServer(t)
	seedAuthorsAndPosts(t, store, registry)
	if err := admin.CreateAccount(t.Context(), store, "editor", "correct-password"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	loginPage := fetch(t, browser, server.URL+"/admin/login/")
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(loginPage)
	if csrf == nil {
		t.Fatalf("the admin login page has no CSRF token")
	}
	loggedIn, err := browser.PostForm(server.URL+"/admin/login/", url.Values{
		"username": {"editor"}, "password": {"correct-password"}, "csrf_token": {csrf[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	loggedIn.Body.Close()
	return fetch(t, browser, server.URL+"/admin/post/"), fetch(t, browser, server.URL+"/admin/post/new/")
}

// changelistColumns returns the header cell texts of the changelist, in order.
func changelistColumns(page string) []string {
	var out []string
	for _, match := range adminHeaderCell.FindAllStringSubmatch(page, -1) {
		out = append(out, strings.TrimSpace(adminTag.ReplaceAllString(match[1], "")))
	}
	return out
}

// formFields returns the ids of the add form's field inputs, in order.
func formFields(page string) []string {
	var out []string
	for _, match := range adminFieldID.FindAllStringSubmatch(page, -1) {
		out = append(out, match[1])
	}
	return out
}

// TestPostAdminShowsNoAuthorRelationField pins the Post admin's columns and
// form fields in full. The Author relation is not a column, so it must not
// appear in either list; the AuthorID column and field are the only author
// entries, as before the relation was added.
func TestPostAdminShowsNoAuthorRelationField(t *testing.T) {
	list, form := adminPostPages(t)

	// Control: the known columns and fields are found in the same pages.
	if cols := changelistColumns(list); !slices.Contains(cols, "Title") {
		t.Fatalf("control: changelist columns %q do not include Title", cols)
	}
	if fields := formFields(form); !slices.Contains(fields, "Title") {
		t.Fatalf("control: form fields %q do not include Title", fields)
	}

	wantColumns := []string{"", "Title", "Author ID", "Created At", "Actions"}
	if got := changelistColumns(list); !slices.Equal(got, wantColumns) {
		t.Errorf("changelist columns = %q, want %q", got, wantColumns)
	}
	wantFields := []string{"Title", "Body", "AuthorID", "CreatedAt"}
	if got := formFields(form); !slices.Equal(got, wantFields) {
		t.Errorf("form fields = %q, want %q", got, wantFields)
	}
}

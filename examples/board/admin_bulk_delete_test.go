package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestAdminBulkDeleteRemovesSelectedPostsAndTheirComments(t *testing.T) {
	app := newTestApp(t)
	ana := app.seedBoardAccount(t, "ana@example.com", true)
	gone := app.seedBoardPost(t, "Post to remove", ana)
	alsoGone := app.seedBoardPost(t, "Another post to remove", ana)
	kept := app.seedBoardPost(t, "Post to keep", ana)
	app.seedBoardComment(t, gone, "Cyrus", "on a removed post")
	app.seedBoardComment(t, kept, "Deepa", "on a kept post")
	ops := adminBrowser(t, app)

	list := ops.do("GET", "/admin/post/", nil).Body.String()
	if !strings.Contains(list, `action="/admin/post/delete-selected/"`) {
		t.Fatalf("the Post changelist does not offer bulk delete:\n%s", list)
	}

	rec := ops.submitTo("/admin/post/", "/admin/post/delete-selected/", url.Values{
		"pk":    {strconv.FormatInt(gone, 10), strconv.FormatInt(alsoGone, 10)},
		"count": {"2"},
	})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/post/" {
		t.Fatalf("status = %d, Location = %q, want 302 to /admin/post/: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}

	// The outcome arrives in the flash cookie the browser holds.
	posts := ops.do("GET", "/admin/post/", nil).Body.String()
	if !strings.Contains(posts, "2 posts deleted") || !strings.Contains(posts, "Post to keep") ||
		strings.Contains(posts, "Post to remove") || strings.Contains(posts, "Another post to remove") {
		t.Fatalf("the Post changelist after the delete is wrong:\n%s", posts)
	}
	comments := ops.do("GET", "/admin/comment/", nil).Body.String()
	if strings.Contains(comments, "Cyrus") || !strings.Contains(comments, "Deepa") {
		t.Fatalf("comments on removed posts should go with them, others stay:\n%s", comments)
	}
}

func TestAdminBulkDeleteIsAvailableOnComments(t *testing.T) {
	app := newTestApp(t)
	ana := app.seedBoardAccount(t, "ana@example.com", true)
	post := app.seedBoardPost(t, "Post", ana)
	first := app.seedBoardComment(t, post, "Cyrus", "one")
	second := app.seedBoardComment(t, post, "Deepa", "two")
	ops := adminBrowser(t, app)

	rec := ops.submitTo("/admin/comment/", "/admin/comment/delete-selected/", url.Values{
		"pk":    {strconv.FormatInt(first, 10), strconv.FormatInt(second, 10)},
		"count": {"2"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body)
	}
	if body := ops.do("GET", "/admin/comment/", nil).Body.String(); strings.Contains(body, "Cyrus") || strings.Contains(body, "Deepa") {
		t.Fatalf("comments should be gone:\n%s", body)
	}
}

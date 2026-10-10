package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/admin"

	"board/apps/posts"
)

// seedBoardAccount inserts an account, active or not, and returns its ID.
func (a *testApp) seedBoardAccount(t *testing.T, email string, active bool) int64 {
	t.Helper()
	meta, _ := a.registry.Models().Get("Account")
	account := accounts.Account{Email: email, Active: active, CreatedAt: time.Now().UTC()}
	if err := a.store.Create(t.Context(), meta, &account); err != nil {
		t.Fatal(err)
	}
	return account.ID
}

// seedBoardPost inserts a post written by accountID and returns its ID.
func (a *testApp) seedBoardPost(t *testing.T, title string, accountID int64) int64 {
	t.Helper()
	meta, _ := a.registry.Models().Get("Post")
	post := posts.Post{Title: title, Body: "body", AccountID: accountID, CreatedAt: time.Now().UTC()}
	if err := a.store.Create(t.Context(), meta, &post); err != nil {
		t.Fatal(err)
	}
	return post.ID
}

// seedBoardComment inserts a comment on postID and returns its ID.
func (a *testApp) seedBoardComment(t *testing.T, postID int64, author, body string) int64 {
	t.Helper()
	meta, _ := a.registry.Models().Get("Comment")
	comment := posts.Comment{PostID: postID, Author: author, Body: body, CreatedAt: time.Now().UTC()}
	if err := a.store.Create(t.Context(), meta, &comment); err != nil {
		t.Fatal(err)
	}
	return comment.ID
}

// adminBrowser returns a browser signed in to the admin as a staff account.
func adminBrowser(t *testing.T, app *testApp) *browser {
	t.Helper()
	const username, password = "ops", "correct horse battery"
	if err := admin.CreateAccount(t.Context(), app.store, username, password); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, app, false)
	rec := b.submit("/admin/login/", url.Values{"username": {username}, "password": {password}})
	if rec.Code != http.StatusFound {
		t.Fatalf("admin login: status = %d, want 302: %s", rec.Code, rec.Body)
	}
	return b
}

func TestAdminChangelistFilters(t *testing.T) {
	app := newTestApp(t)
	ana := app.seedBoardAccount(t, "ana@example.com", true)
	bo := app.seedBoardAccount(t, "bo@example.com", false)
	app.seedBoardPost(t, "Post by Ana", ana)
	app.seedBoardPost(t, "Post by Bo", bo)
	ops := adminBrowser(t, app)

	t.Run("boolean filter narrows accounts to inactive ones", func(t *testing.T) {
		body := ops.do("GET", "/admin/account/?filter_active=no", nil).Body.String()
		if strings.Contains(body, "ana@example.com") || !strings.Contains(body, "bo@example.com") {
			t.Fatalf("filter_active=no should show only bo:\n%s", body)
		}
	})

	t.Run("foreign key filter offers accounts and narrows posts", func(t *testing.T) {
		all := ops.do("GET", "/admin/post/", nil).Body.String()
		if !strings.Contains(all, `<option value="`+strconv.FormatInt(bo, 10)+`">bo@example.com</option>`) {
			t.Fatalf("author filter should offer accounts labelled by email:\n%s", all)
		}
		body := ops.do("GET", "/admin/post/?filter_account_id="+strconv.FormatInt(bo, 10), nil).Body.String()
		if strings.Contains(body, "Post by Ana") || !strings.Contains(body, "Post by Bo") {
			t.Fatalf("author filter should keep only Bo's post:\n%s", body)
		}
	})

	t.Run("foreign key filter narrows comments to one post", func(t *testing.T) {
		postID := app.seedBoardPost(t, "Post with comments", ana)
		app.seedBoardComment(t, postID, "Cyrus", "first")
		app.seedBoardComment(t, postID, "Deepa", "second")
		body := ops.do("GET", "/admin/comment/?filter_post_id="+strconv.FormatInt(postID, 10), nil).Body.String()
		if !strings.Contains(body, "Cyrus") || !strings.Contains(body, "Deepa") {
			t.Fatalf("comments on the post should be listed:\n%s", body)
		}
		unknown := ops.do("GET", "/admin/comment/?filter_post_id="+strconv.FormatInt(postID+1000, 10), nil).Body.String()
		if !strings.Contains(unknown, "Cyrus") || !strings.Contains(unknown, "Deepa") {
			t.Fatalf("a nonexistent post ID is ignored, so every comment shows:\n%s", unknown)
		}
	})
}

package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"

	"board/apps/posts"
)

// page is what every template receives.
type page struct {
	Title    string
	Posts    []posts.PostSummary
	Post     posts.Post
	Author   string
	Comments []posts.Comment

	// Set by render on every page.
	Account   *accounts.Account // nil when nobody is logged in
	CSRFToken string
	Path      string

	// Form state, for re-rendering a form with an error.
	Error string
	Form  struct{ Title, Body string }
}

func (p *pages) render(ctx *tango.Context, status int, name string, data page) error {
	account, ok, err := accounts.CurrentAccount(ctx, p.store, accounts.DefaultSessionCookieName)
	if err != nil {
		return err
	}
	if ok {
		data.Account = &account
		data.CSRFToken = csrfToken(ctx)
	}
	data.Path = ctx.Request().URL.Path
	return ctx.HTML(status, p.tmpl, name, data)
}

func (p *pages) notFound(ctx *tango.Context) error {
	return p.render(ctx, http.StatusNotFound, "not_found", page{Title: "Not found"})
}

func (p *pages) home(ctx *tango.Context) error {
	summaries, err := posts.Summaries(ctx.Context(), p.store)
	if err != nil {
		return err
	}
	return p.render(ctx, http.StatusOK, "home", page{Posts: summaries})
}

func (p *pages) post(ctx *tango.Context) error {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		return p.notFound(ctx)
	}
	var post posts.Post
	err = p.store.Get(ctx.Context(), p.postMeta, id, &post)
	if errors.Is(err, db.ErrNotFound) {
		return p.notFound(ctx)
	}
	if err != nil {
		return err
	}
	var comments []posts.Comment
	err = p.store.List(ctx.Context(), p.commentMeta, db.Query{
		Where:   []db.Condition{{Field: "PostID", Op: db.OpEq, Value: id}},
		OrderBy: []string{"CreatedAt"},
	}, &comments)
	if err != nil {
		return err
	}

	data := page{Title: post.Title, Post: post, Comments: comments}
	if post.AccountID != 0 { // 0 means "no owner": posts created before accounts existed
		var author accounts.Account
		if err := p.store.Get(ctx.Context(), p.accountMeta, post.AccountID, &author); err != nil {
			return err
		}
		data.Author = displayName(author.Email)
	}
	return p.render(ctx, http.StatusOK, "post", data)
}

func (p *pages) newPost(ctx *tango.Context) error {
	return p.render(ctx, http.StatusOK, "new_post", page{Title: "New post"})
}

func (p *pages) createPost(ctx *tango.Context) error {
	if !validCSRF(ctx) {
		return ctx.JSON(http.StatusForbidden, map[string]string{"error": "invalid CSRF token"})
	}
	accountID, _, err := accounts.CurrentAccountID(ctx, p.store, accounts.DefaultSessionCookieName)
	if err != nil {
		return err
	}

	data := page{Title: "New post"}
	data.Form.Title = strings.TrimSpace(ctx.Request().PostFormValue("title"))
	data.Form.Body = strings.TrimSpace(ctx.Request().PostFormValue("body"))
	if data.Form.Title == "" {
		data.Error = "Give your post a title."
		return p.render(ctx, http.StatusBadRequest, "new_post", data)
	}

	post := posts.Post{
		Title:     data.Form.Title,
		Body:      data.Form.Body,
		AccountID: accountID,
		CreatedAt: time.Now().UTC(),
	}
	if err := p.store.Create(ctx.Context(), p.postMeta, &post); err != nil {
		return err
	}
	if err := p.feed.Publish(ctx.Context(), post.ID, post.Title); err != nil {
		ctx.Logger().Warn("live feed publish failed", "post", post.ID, "err", err)
	}
	target, err := p.url("post", "id", post.ID)
	if err != nil {
		return err
	}
	return ctx.Redirect(target)
}

func (p *pages) createComment(ctx *tango.Context) error {
	if !validCSRF(ctx) {
		return ctx.JSON(http.StatusForbidden, map[string]string{"error": "invalid CSRF token"})
	}
	postID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		return p.notFound(ctx)
	}
	account, _, err := accounts.CurrentAccount(ctx, p.store, accounts.DefaultSessionCookieName)
	if err != nil {
		return err
	}
	target, err := p.url("post", "id", postID)
	if err != nil {
		return err
	}

	body := strings.TrimSpace(ctx.Request().PostFormValue("body"))
	if body == "" {
		return ctx.Redirect(target) // nothing to add
	}
	comment := posts.Comment{
		PostID:    postID,
		Author:    displayName(account.Email),
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	err = p.store.Create(ctx.Context(), p.commentMeta, &comment)
	if errors.Is(err, db.ErrInvalidForeignKey) {
		return p.notFound(ctx)
	}
	if err != nil {
		return err
	}
	return ctx.Redirect(target + "#comments")
}

// url lets templates build links from route names:
// {{url "post" "id" .ID}} renders /p/42/. The Reverser can only be built
// once every app has registered, so it's created on first use.
func (p *pages) url(name string, pairs ...any) (string, error) {
	p.reverserOnce.Do(func() {
		p.reverser, p.reverserErr = p.routes.Reverser()
	})
	if p.reverserErr != nil {
		return "", p.reverserErr
	}
	if len(pairs)%2 != 0 {
		return "", fmt.Errorf("url %q: params must come in name/value pairs", name)
	}
	params := tango.Params{}
	for i := 0; i < len(pairs); i += 2 {
		params[fmt.Sprint(pairs[i])] = fmt.Sprint(pairs[i+1])
	}
	return p.reverser.Reverse(name, params)
}

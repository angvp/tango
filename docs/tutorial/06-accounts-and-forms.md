# Tutorial, part 6: accounts and forms

Continuing from [part 5](05-html-pages.md), this part lets people sign up, log in, and write on the board from the browser. You'll install tanGO's `accounts` app for registration and login, give every post an owner, protect pages behind a login, and handle HTML forms safely with CSRF tokens.

## Install `accounts`

`accounts` is an optional app that ships with tanGO: email-and-password sign-up, login, and logout pages, with sessions stored in the database. Install it in `main.go`, before `web` (which will look up its model):

```go
InstalledApps: []tango.App{
	posts.New(store),
	accounts.New(store),
	web.New(store),
	admin.New(store),
},
```

(import `"github.com/angvp/tango/accounts"`.) It mounts `/accounts/register/`, `/accounts/login/`, and `/accounts/logout/`, and registers two models, `Account` and `AccountSession`. It deliberately stays separate from the admin's own users: an admin account can't log in to the board, and a board account can't open the admin.

## Give posts an owner

A post now belongs to the account that wrote it — another foreign key, this time pointing at a model from a different app:

```go
// apps/posts/models.go
type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	AccountID int64 `tango:"fk=Account,index"`
	CreatedAt time.Time
}

type Comment struct {
	ID        int64 `tango:"pk"`
	PostID    int64 `tango:"fk=Post,index"`
	Author    string
	Body      string
	CreatedAt time.Time
}
```

## Migrate

```sh
tango makemigrations
tango migrate
```

This writes and applies two migrations: the `accounts` tables, and a new `account_id` column on `post`. The posts you created in earlier parts have no owner, so their `account_id` is `NULL`. tanGO fields are never nil: a `NULL` column reads back as the field's zero value, so those posts load with `AccountID` 0. That's the same "unset" a foreign key has when you leave it at zero, which tanGO also stores as `NULL`. The old posts keep working everywhere; they just don't belong to anyone.

## Who is posting?

The JSON API's `createPost` can no longer create a post without an owner. For now it uses the same login as the browser: `accounts.CurrentAccountID` reads the session cookie and answers with the logged-in account, or `ok == false` if there isn't one:

```go
// apps/posts/views.go
func createPost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		accountID, ok, err := accounts.CurrentAccountID(ctx, store, accounts.DefaultSessionCookieName)
		if err != nil {
			return err
		}
		if !ok {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "log in to post"})
		}

		var post Post
		if err := ctx.Bind(&post); err != nil {
			return badRequest(ctx, "invalid JSON body")
		}
		if post.Title == "" {
			return badRequest(ctx, "title is required")
		}
		post.ID = 0                // the database assigns IDs, never the client
		post.AccountID = accountID // the poster is whoever is logged in
		post.CreatedAt = time.Now().UTC()
		if err := store.Create(ctx.Context(), meta, &post); err != nil {
			return err
		}
		return ctx.JSON(http.StatusCreated, post)
	}
}
```

As before, the owner comes from the server's side of the request, never the JSON body — a client sending `"AccountID": 999` is simply overwritten. Using the browser's session cookie for a JSON endpoint is safe here because `accounts` sets it `SameSite=Lax`, so other sites can't make a visitor's browser send it with a cross-site `POST`. [Part 7](07-api-tokens-and-rate-limits.md) gives the API its own tokens, which is what non-browser clients need.

## Sessions and CSRF in the `web` app

Put the login-related helpers in their own file, `apps/web/session.go`:

```go
package web

import (
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
)

const loginPath = "/accounts/login/"

// requireLogin sends visitors without a valid session to the login page,
// with ?next= pointing back at the page they wanted.
func (p *pages) requireLogin(view tango.View) tango.View {
	return accounts.RequireLogin(p.store, accounts.DefaultSessionCookieName, loginPath, view)
}

// sessionToken is the visitor's session cookie value, or "" without one.
func sessionToken(ctx *tango.Context) string {
	cookie, err := ctx.Request().Cookie(accounts.DefaultSessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// csrfToken is derived from the session token, so it's unique per session
// and can only be produced by someone who holds the session cookie.
func csrfToken(ctx *tango.Context) string {
	if token := sessionToken(ctx); token != "" {
		return auth.DeriveCSRFToken(token)
	}
	return ""
}

// validCSRF checks the csrf_token field of a submitted form.
func validCSRF(ctx *tango.Context) bool {
	token := sessionToken(ctx)
	return token != "" && auth.VerifyCSRFToken(ctx.Request().PostFormValue("csrf_token"), token)
}

// displayName shows the part of an email before the @, so pages never
// publish anyone's full address.
func displayName(email string) string {
	name, _, _ := strings.Cut(email, "@")
	return name
}
```

`accounts.RequireLogin` is a **View wrapper**: it takes a view and returns a new one that checks the session first and sends visitors without one to the login page. After logging in, they land back on the page they asked for.

Every form that changes something needs a CSRF token — otherwise another site could post a form to your board, and the visitor's browser would helpfully attach their session cookie. The token here is derived from the session token itself, so it's different for every session, can't be guessed without the cookie, and doesn't need to be stored anywhere. `auth.VerifyCSRFToken` recomputes it and compares.

## Every page knows who's logged in

The `page` struct grows the fields every page needs for a logged-in visitor, plus form state for re-rendering a form with an error. `render` fills in the account, CSRF token, and current path for every page, so no view has to remember to:

```go
// apps/web/views.go
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
```

The header in `apps/web/templates/layout.gohtml` now shows who's logged in:

```html
{{define "header"}}<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{with .Title}}{{.}} · {{end}}Board</title>
  <link rel="stylesheet" href="/static/board.css">
</head>
<body>
  <header>
    <a class="brand" href="{{url "home"}}">Board</a>
    <nav>
      {{if .Account}}
        <a href="{{url "new_post"}}">New post</a>
        <form method="post" action="/accounts/logout/">
          <button type="submit">Log out {{.Account.Email}}</button>
        </form>
      {{else}}
        <a href="/accounts/login/?next={{.Path}}">Log in</a>
        <a href="/accounts/register/">Sign up</a>
      {{end}}
    </nav>
  </header>
  <main>
{{end}}

{{define "footer"}}
  </main>
</body>
</html>
{{end}}
```

Logging out is a `POST` (a link that logs you out when an image or crawler fetches it would be a bug), so it's a one-button form. The login link carries `next`, so logging in returns you to the page you were reading.

## Writing posts

The new-post page is a form, `apps/web/templates/new_post.gohtml`:

```html
{{define "new_post"}}
{{template "header" .}}
<h1>New post</h1>
{{with .Error}}<p class="error">{{.}}</p>{{end}}
<form method="post" action="{{url "new_post"}}">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <label>Title <input name="title" value="{{.Form.Title}}" required></label>
  <label>Body <textarea name="body" rows="6">{{.Form.Body}}</textarea></label>
  <button type="submit">Publish</button>
</form>
{{template "footer" .}}
{{end}}
```

Two views serve it: `GET /new/` shows the form, and `POST /new/` handles it. Both are wrapped in `requireLogin`, and the routes list in `apps/web/app.go` now reads:

```go
return registry.Routes().Include("/", tango.URLs{
	tango.Path("GET", "/", p.home, tango.Name("home")),
	tango.Path("GET", "/p/{id}/", p.post, tango.Name("post")),
	tango.Path("GET", "/new/", p.requireLogin(p.newPost), tango.Name("new_post")),
	tango.Path("POST", "/new/", p.requireLogin(p.createPost)),
	tango.Path("POST", "/p/{id}/comments/", p.requireLogin(p.createComment), tango.Name("add_comment")),
	tango.Path("GET", "/static/*", serveStatic(), tango.Name("static")),
})
```

The handler checks the CSRF token first, then validates. When the title is missing, it renders the same form again with a message and what the visitor typed, and a `400` — nobody likes retyping a post. On success it redirects to the new post, using the same reverse lookup the templates use:

```go
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
	target, err := p.url("post", "id", post.ID)
	if err != nil {
		return err
	}
	return ctx.Redirect(target)
}
```

`ctx.Request().PostFormValue` reads form fields — `ctx.Bind` is for JSON bodies, forms go through the standard request. Redirecting after a successful `POST` means refreshing the page afterwards doesn't submit the post twice.

## Comments from the browser

The post page shows the author and, for logged-in visitors, a comment form. The new `apps/web/templates/post.gohtml`:

```html
{{define "post"}}
{{template "header" .}}
<article>
  <h1>{{.Post.Title}}</h1>
  <p class="meta">{{with .Author}}by {{.}} · {{end}}{{.Post.CreatedAt.Format "Jan 2, 2006"}}</p>
  <p>{{.Post.Body}}</p>
</article>
<section id="comments">
  <h2>Comments</h2>
  {{range .Comments}}
    <div class="comment"><strong>{{.Author}}</strong> {{.Body}}</div>
  {{else}}
    <p>No comments yet.</p>
  {{end}}
  {{if .Account}}
    <form method="post" action="{{url "add_comment" "id" .Post.ID}}">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <label>Add a comment <textarea name="body" rows="3" required></textarea></label>
      <button type="submit">Comment</button>
    </form>
  {{else}}
    <p><a href="/accounts/login/?next={{.Path}}">Log in</a> to comment.</p>
  {{end}}
</section>
<p><a href="{{url "home"}}">← All posts</a></p>
{{template "footer" .}}
{{end}}
```

The comment view follows the same shape. The author's name comes from their account, not from a form field:

```go
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
```

The post view looks up the author's account to show their name. That's a second `store.Get`, on a model registered by another app:

```go
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
```

`displayName` shows only the part of the email before the `@`, so the board never publishes anyone's address. Add an `accountMeta model.ModelMeta` field to `pages`, and look it up in `New` next to the other two: `p.accountMeta, _ = registry.Models().Get("Account")`.

## Manage accounts in the admin

`accounts` doesn't register its models with the admin for you. Do it in `web`'s `New`, before the routes:

```go
// Accounts are managed from the admin like any other model.
if err := registry.Admin().Register(accounts.Account{}, admin.Options{
	ListDisplay: []string{"Email", "Active", "CreatedAt"},
	Search:      []string{"Email"},
	ReadOnly:    []string{"PasswordHash", "CreatedAt"},
	Label:       "Email",
}); err != nil {
	return err
}
```

`ReadOnly` keeps the password hash out of the edit form, and `Label: "Email"` is what the admin shows wherever an account is referenced — including the owner dropdown on the post form. Unticking `Active` on an account in the admin logs it out everywhere immediately: `accounts` checks it on every request, not just at login.

A few lines of CSS for the new header and forms, appended to `apps/web/static/board.css`:

```css
header { display: flex; justify-content: space-between; align-items: center; gap: 1rem; }
header .brand { font-weight: 800; font-size: 1.25rem; text-decoration: none; }
nav { display: flex; gap: 0.75rem; align-items: center; }
nav form { margin: 0; }
label { display: block; margin: 0.75rem 0; }
input, textarea { display: block; width: 100%; font: inherit; padding: 0.4rem; box-sizing: border-box; }
.error { color: #b91c1c; }
```

## Try it

```sh
go run .
```

1. Open `http://localhost:8000/` and click **Sign up**. Registering logs you straight in.
2. Click **New post**, submit it with an empty title to see the error, then publish it properly. You land on your post, marked "by" your name.
3. Add a comment. Log out, and the comment form turns into a "Log in to comment" link.
4. Try the API: `curl -X POST http://localhost:8000/posts/ -d '{"title":"x"}'` now answers `401 {"error":"log in to post"}`.

**Part 7** gives the JSON API its own authentication — tokens that a script or a phone app can send — and puts a rate limit in front of it.

Continue: [Tutorial, part 7: API tokens and rate limits](07-api-tokens-and-rate-limits.md)

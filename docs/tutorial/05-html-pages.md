# Tutorial, part 5: HTML pages

Continuing from [part 4](04-comments-and-foreign-keys.md), this part gives the board real pages: a front page listing posts and a page per post with its comments. tanGO has no template language of its own — views render the standard library's `html/template` with `ctx.HTML` — so everything here is ordinary Go you may already know. You'll also build links from route names instead of hard-coding URLs, and serve a stylesheet.

## A second app for the pages

The JSON API lives in `posts`. The pages go in their own app, which keeps each app's job small and shows how apps share data: `web` owns no models, it reads the ones `posts` registered.

```sh
tango newapp web
mkdir -p apps/web/templates apps/web/static
```

Install it in `main.go` after `posts` — order matters, because `web` looks up models that `posts` registers:

```go
InstalledApps: []tango.App{
	posts.New(store),
	web.New(store),
	admin.New(store),
},
```

(import `"board/apps/web"`.)

## Share the summary query

The front page needs the same "posts with comment counts" query as `GET /posts/`. Move it out of the view into an exported function in `apps/posts/queries.go`, so both apps call it:

```go
// apps/posts/queries.go
package posts

import (
	"context"
	"time"

	"github.com/angvp/tango/db"
)

// PostSummary is one post plus how many comments it has. It isn't a
// registered model — just a shape to scan query results into.
type PostSummary struct {
	ID           int64
	Title        string
	CreatedAt    time.Time
	CommentCount int
}

// Summaries returns every post, newest first, with its comment count.
func Summaries(ctx context.Context, store *db.Store) ([]PostSummary, error) {
	var posts []PostSummary
	err := store.Query(ctx, &posts, `
		SELECT p.id, p.title, p.created_at, COUNT(c.id) AS comment_count
		FROM post p
		LEFT JOIN comment c ON c.post_id = p.id
		GROUP BY p.id, p.title, p.created_at
		ORDER BY p.created_at DESC`)
	return posts, err
}
```

Then replace `postSummary` and `listPosts` in `apps/posts/views.go` with the shorter version:

```go
func listPosts(store *db.Store) tango.View {
	return func(ctx *tango.Context) error {
		posts, err := Summaries(ctx.Context(), store)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, posts)
	}
}
```

## Templates

Templates live next to the app and are embedded into the binary with `go:embed`, so there are no files to ship alongside it. Put a shared header and footer in `apps/web/templates/layout.gohtml`:

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
  <header><a href="{{url "home"}}">Board</a></header>
  <main>
{{end}}

{{define "footer"}}
  </main>
</body>
</html>
{{end}}
```

Every page receives the same `page` struct (defined below), so the header can read `.Title` whichever page it's in. Each page is a named template that wraps its content in the two. The front page, `apps/web/templates/home.gohtml`:

```html
{{define "home"}}
{{template "header" .}}
<h1>Latest posts</h1>
{{range .Posts}}
  <article class="summary">
    <h2><a href="{{url "post" "id" .ID}}">{{.Title}}</a></h2>
    <p class="meta">{{.CreatedAt.Format "Jan 2, 2006"}} · {{.CommentCount}} comment{{if ne .CommentCount 1}}s{{end}}</p>
  </article>
{{else}}
  <p>No posts yet.</p>
{{end}}
{{template "footer" .}}
{{end}}
```

A post with its comments, `apps/web/templates/post.gohtml`:

```html
{{define "post"}}
{{template "header" .}}
<article>
  <h1>{{.Post.Title}}</h1>
  <p class="meta">{{.Post.CreatedAt.Format "Jan 2, 2006"}}</p>
  <p>{{.Post.Body}}</p>
</article>
<section>
  <h2>Comments</h2>
  {{range .Comments}}
    <div class="comment"><strong>{{.Author}}</strong> {{.Body}}</div>
  {{else}}
    <p>No comments yet.</p>
  {{end}}
</section>
<p><a href="{{url "home"}}">← All posts</a></p>
{{template "footer" .}}
{{end}}
```

And a page for missing posts, `apps/web/templates/not_found.gohtml`:

```html
{{define "not_found"}}
{{template "header" .}}
<h1>Not found</h1>
<p>There's nothing here. <a href="{{url "home"}}">Back to the board</a>.</p>
{{template "footer" .}}
{{end}}
```

`html/template` escapes everything it prints for the context it's printed in. A post titled `<script>alert(1)</script>` shows up as text, not as a script, with no extra work from you — which matters, since titles, bodies, and comment authors all come straight from users.

## Links from route names

The templates never spell out a URL. `{{url "post" "id" .ID}}` asks tanGO's reverse lookup for the route named `post` with its `{id}` filled in, so if you later move posts from `/p/{id}/` to `/posts/{id}/`, you change one route and every link follows.

Routes included under `/` have no namespace, so their names are just `home` and `post` (the JSON routes, included under `/posts/`, are `posts:list`, `posts:detail`, and so on).

## The app

`apps/web/app.go` parses the templates once, while the app registers, so a template syntax error fails `tango check` instead of the first request:

```go
// Package web serves the board's HTML pages. It owns no models: it reads
// the posts app's data and renders it with html/template.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"sync"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

//go:embed templates/*.gohtml
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

type pages struct {
	store       *db.Store
	postMeta    model.ModelMeta
	commentMeta model.ModelMeta
	tmpl        *template.Template

	routes       *tango.RouteRegistry
	reverserOnce sync.Once
	reverser     tango.Reverser
	reverserErr  error
}

func New(store *db.Store) tango.App {
	return tango.NewApp("web", func(registry *tango.Registry) error {
		p := &pages{store: store, routes: registry.Routes()}
		p.postMeta, _ = registry.Models().Get("Post")
		p.commentMeta, _ = registry.Models().Get("Comment")

		tmpl, err := template.New("").Funcs(template.FuncMap{"url": p.url}).
			ParseFS(templatesFS, "templates/*.gohtml")
		if err != nil {
			return err
		}
		p.tmpl = tmpl

		return registry.Routes().Include("/", tango.URLs{
			tango.Path("GET", "/", p.home, tango.Name("home")),
			tango.Path("GET", "/p/{id}/", p.post, tango.Name("post")),
			tango.Path("GET", "/static/*", serveStatic(), tango.Name("static")),
		})
	})
}
```

The views go in `apps/web/views.go`. Every template gets the same `page` struct, filled in with whatever that page needs; a small `render` helper wraps `ctx.HTML(status, templates, name, data)`. `ctx.HTML` renders into a buffer first, so a template that fails halfway through becomes an ordinary error — a clean `500` — rather than half a page:

```go
// apps/web/views.go
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"

	"board/apps/posts"
)

// page is what every template receives.
type page struct {
	Title    string
	Posts    []posts.PostSummary
	Post     posts.Post
	Comments []posts.Comment
}

func (p *pages) render(ctx *tango.Context, status int, name string, data page) error {
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
	return p.render(ctx, http.StatusOK, "post", page{Title: post.Title, Post: post, Comments: comments})
}
```

The `url` template function, at the bottom of `views.go`, is where reverse lookup happens. A `Reverser` can only be built after every installed app has registered its routes — later than `web`'s own `Register` runs — so `url` builds it on first use and keeps it:

```go
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
```

A misspelled route name makes `url` return an error, which fails the render — you find out the first time the page loads, not from a user clicking a dead link.

## Static files

An app serves its own static files through an ordinary route — there's no separate asset system to configure. Add a stylesheet at `apps/web/static/board.css`:

```css
body { max-width: 40rem; margin: 0 auto; padding: 1rem; font: 17px/1.6 system-ui, sans-serif; color: #1f2937; }
header a { font-weight: 800; font-size: 1.25rem; color: #013b8a; text-decoration: none; }
a { color: #013b8a; }
.meta { color: #6b7280; font-size: 0.9rem; margin-top: -0.5rem; }
.summary h2 { margin-bottom: 0.25rem; }
.comment { padding: 0.5rem 0; border-top: 1px solid #e5e7eb; }
```

and the view that serves it, at the bottom of `apps/web/app.go`:

```go
func serveStatic() tango.View {
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/static/", http.FileServer(http.FS(assets)))
	return func(ctx *tango.Context) error {
		files.ServeHTTP(ctx.ResponseWriter(), ctx.Request())
		return nil
	}
}
```

`ctx.ResponseWriter()` and `ctx.Request()` are the escape hatch to plain `net/http`: anything that already works as an `http.Handler`, like the standard file server, plugs straight into a view.

## Try it

```sh
go run . -check
go run .
```

Open `http://localhost:8000/`. Every post you've created through the API is listed with its comment count; click one to read it and its comments. `http://localhost:8000/p/999/` shows the not-found page with a real `404` status. The JSON API at `/posts/` works exactly as before — pages and API are two apps reading the same tables.

The board can be read but not written from the browser yet. **Part 6** adds accounts: people sign up, log in, and write posts through a form.

Continue: [Tutorial, part 6: accounts and forms](06-accounts-and-forms.md)

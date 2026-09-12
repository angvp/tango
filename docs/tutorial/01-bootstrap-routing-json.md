# Tutorial, part 1: bootstrap, routing, JSON views

This tutorial builds one small application — a bulletin board called `board` — from an empty directory to a running app with persistence, migrations, and an admin. This part covers scaffolding, routing, and JSON views. By the end you'll have a running `go run .` serving a real route.

## Scaffold the project

```sh
tango newproject board
cd board
```

This creates a new Go module (`board`) with a `main.go` pre-wired for SQLite: it opens `app.db`, constructs a `db.Store`, and implements the flag-dispatch convention every tanGO app's `main.go` follows (`-check`, `-tango-dump-models`, `-tango-status`, `-migrate[-down]`). No app is registered yet — `InstalledApps` starts empty. Confirm it runs:

```sh
go run . -check
# check passed
```

## Add an app

tanGO groups related models and routes into **apps** — plain Go packages implementing a two-method interface:

```go
type App interface {
	Name() string
	Register(*Registry) error
}
```

Scaffold one:

```sh
tango newapp posts
```

This writes `apps/posts/app.go` with a stub `Name()`/`Register()`. It does **not** edit `main.go` for you — wiring a new app in is always one line you write yourself, so nothing about your project's composition is hidden:

```go
// main.go
config := tango.Config{
	InstalledApps: []tango.App{posts.App{}},
	Addr:          ":8000",
}
```

(add the import for `"board/apps/posts"` alongside it.)

## Named routes

Inside `apps/posts/app.go`, `Register` declares routes via `Include`, a namespace prefix, and a list of `Path` declarations:

```go
package posts

import "github.com/angvp/tango"

type App struct{}

func (App) Name() string { return "posts" }

func (App) Register(registry *tango.Registry) error {
	return registry.Routes().Include("/posts/", tango.URLs{
		tango.Path("GET", "/", listPosts, tango.Name("list")),
		tango.Path("GET", "/{id}/", getPost, tango.Name("detail")),
	})
}
```

Route names are namespace-qualified: the routes above are known internally as `posts:list` and `posts:detail`. Reverse lookup (turning a route name back into a URL) uses this qualified name — see the [routing guide](../guides/routing-and-reverse-lookup.md) for the full API.

## JSON views

A `View` is `func(*tango.Context) error`. `Context` gives you `Param` (path parameters), `Query` (query string values), `Bind` (decode a request body), and `JSON` (write a JSON response):

```go
func listPosts(ctx *tango.Context) error {
	return ctx.JSON(200, map[string]string{"posts": "none yet"})
}

func getPost(ctx *tango.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(200, map[string]string{"id": id})
}
```

A view returning a non-nil error maps to a fixed 500 JSON response; the error is logged server-side but never exposed to the client, so it's safe to return errors from database calls directly (added in [part 2](02-persistence-migrations.md)).

## Run it

```sh
go run . -check     # validates registration and route compilation, doesn't start the server
go run .            # starts serving on :8000
curl http://localhost:8000/posts/
curl http://localhost:8000/posts/1/
```

You now have a running tanGO app with two named, working JSON routes. **Part 2** adds a real model and persists it through generated migrations.

Continue: [Tutorial, part 2: persistence and migrations](02-persistence-migrations.md)

# Tutorial, part 1: bootstrap, routing, JSON views

This tutorial builds one small application — a bulletin board called `board` — from an empty directory to a running app with persistence, migrations, and an admin. This part covers scaffolding, routing, and JSON views. By the end you'll have a running `go run .` serving a real route.

## Scaffold the project

```sh
tango newproject board
cd board
```

This creates a new Go module (`board`) with a `main.go` pre-wired for SQLite and the admin app. Admin accounts live in the database rather than a generated file — you'll create one with `tango admin create` in part 3, once the admin's own tables exist. The generated `main.go` stays small: it opens the database, builds `Config`, calls `tango.DispatchFlags`, then calls `tango.ServeContext` under a context that Ctrl-C or `SIGTERM` cancels, so the server stops gracefully. Confirm it runs:

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

This writes `apps/posts/app.go` with a stub `Name()`/`Register()`, and prints the line to add to `main.go`. It does **not** edit `main.go` for you — wiring a new app in is always one line you write yourself, so nothing about your project's composition is hidden:

```go
// main.go (as of part 1)
config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
config.InstalledApps = []tango.App{
	posts.App{},
	// keep the generated admin.New(...) entry after your apps
}
```

The generated `main.go` reads the listen address from the environment: `TANGO_ADDR`, else the `PORT` a hosting platform sets, else `:8000`. It also sets up some middleware, which part 9 explains.

(add the import for `"board/apps/posts"` alongside it.)

## Named routes

Inside `apps/posts/app.go`, `Register` declares routes via `Include`, a namespace prefix, and a list of `Path` declarations:

```go
// apps/posts/app.go (as of part 1)
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

A `View` is `func(*tango.Context) error`. `Context` gives you `Param` (path parameters), `Query` (query string values), `Bind` (decode a request body), and `JSON` (write a JSON response). Views live next to the app in `apps/posts/views.go`, which starts with `package posts` and imports `github.com/angvp/tango`:

```go
// apps/posts/views.go (as of part 1)
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

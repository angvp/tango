# tanGO

tanGO is a small, explicit web framework for Go, inspired by Django's ergonomics but built from plain Go structs and interfaces — no code generation, no reflection-heavy magic beyond what's needed to read your model tags, and no hidden configuration.

**Status: v0.1 candidate.** The public API is still settling. Behavior described in this README and the linked docs reflects what's implemented today; anything explicitly marked "planned" or "future" is not yet built. See [Limitations and compatibility](docs/limitations.md) before depending on tanGO for anything beyond experimentation.

## What you get

- An explicit app/registry lifecycle (`Config`, `App`, `Registry`) — nothing boots implicitly.
- A URL dispatcher with named routes and reverse lookup, built on [chi](https://github.com/go-chi/chi) internally (chi itself is never part of your imports).
- `Context` helpers for JSON responses, request binding, redirects, and raw request/response escape hatches.
- A model metadata engine that reads plain Go structs and a handful of tags (`tango:"pk"`, `tango:"unique"`, `tango:"index"`, `tango:"fk=Other"`).
- A minimal `db.Store` for CRUD and raw SQL against SQLite or PostgreSQL, with **minimal ORM foundations**: foreign key fields, two-layer validation, and cascade delete — see [relationships and admin foreign keys](docs/guides/relationships-and-admin-foreign-keys.md).
- Generated, typed Go migrations (`tango makemigrations`/`migrate`/`migrate down`) — no separate schema DSL.
- A server-rendered HTML admin (list/create/edit/delete, pagination, sorting, search) behind a session-cookie login, with CSRF protection and rate-limited login attempts — accounts are managed via `tango admin create/resetpassword/deactivate`.
- A CLI (`tango run`/`check`/`makemigrations`/`migrate`/`newproject`/`newapp`/`tui`/`admin`) that wraps ordinary `go build`/`go run` rather than replacing them.

## Install

```sh
go get github.com/angvp/tango
```

Install the `tango` CLI (optional, but recommended — it wraps `go run`/`go build` with a few conventions the framework expects your `main.go` to implement):

```sh
go install github.com/angvp/tango/cmd/tango@latest
```

## The smallest runnable application

The fastest way to see a real one is [`examples/jsonapi`](examples/jsonapi) — a complete, separate Go module you can copy as a starting point. It was produced with:

```sh
tango newproject jsonapi
cd jsonapi
tango newapp greetings
```

`tango newproject` scaffolds a `go.mod` and a small `main.go` pre-wired for SQLite and the admin app; create your first admin account afterward with `tango admin create <username>`. `tango newapp` scaffolds an app stub; wiring it into `main.go`'s `InstalledApps` is one line you write yourself — tanGO never edits your `main.go` for you. The example's app registers one named route:

```go
func (App) Register(registry *tango.Registry) error {
	return registry.Routes().Include("/greetings/", tango.URLs{
		tango.Path("GET", "/{name}/", hello, tango.Name("hello")),
	})
}

func hello(ctx *tango.Context) error {
	name := ctx.Param("name")
	return ctx.JSON(200, map[string]string{"message": "Hello, " + name + "!"})
}
```

Run it:

```sh
cd examples/jsonapi
go run .
curl http://localhost:8000/greetings/World/
# {"message":"Hello, World!"}
```

## Where to continue

- **[Tutorial](docs/tutorial/01-bootstrap-routing-json.md)** — build one small application from scratch: bootstrap, routing, JSON views, persistence, migrations, and the HTML admin.
- **[Guides](docs/guides/)** — standalone, task-oriented references: project structure, configuration, models and tags, routing, `Context`, persistence, migrations, admin, app checks, dialect setup, reusable apps, and relationships/admin foreign keys.
- **[API reference](docs/reference.md)** — the supported v0.1 public surface, linked to runnable examples. Generated package docs are also available via `go doc` or [pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango) once published.
- **[Examples](examples/)** — `jsonapi` (JSON-only); `api-with-admin` (JSON API and HTML admin sharing the same models, including an `AuthorID`-style foreign key with FK-backed admin editing); `notes-starter` (a keepable starter-style app using the reduced `main.go` shape); and `reusable-greetings`/`reusable-greetings-host` (a reusable tanGO app and a host project installing it, demonstrating contributed migrations and app-owned static assets).
- **[Limitations and compatibility](docs/limitations.md)** — v0.1 non-goals, security boundaries, dialect differences, and APIs still expected to change.

## Verifying the docs

Documentation in this repository is checked, not just written: every checked-in example under `examples/` is built as its own module in CI, so a stale command or import path in a doc snippet gets caught rather than silently drifting from what actually runs. Run the same check locally before sending a documentation change:

```sh
go test ./...
```

`TestExamplesAreIndependentModulesThatCompile` (at the repository root) builds every `examples/*` module and runs its generated `-check` flag; the migration-generation tests in `internal/cli` similarly `go build` a real generated `migrations` package rather than only inspecting its source text.

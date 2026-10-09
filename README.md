# tanGO

[![codecov](https://codecov.io/gh/angvp/tango/branch/main/graph/badge.svg)](https://codecov.io/gh/angvp/tango)

tanGO is a small, explicit web framework for Go, inspired by Django's ergonomics but built from plain Go structs and interfaces. Scaffolding writes plain Go you own. Nothing is generated at runtime, and boot never scans your filesystem; there's no reflection-heavy magic beyond what's needed to read your model tags, and no hidden configuration.

**Status: early development.** The public API is still settling. Behavior described in this README and the linked docs reflects what's implemented today; anything explicitly marked "planned" or "future" is not yet built. See [Versioning and compatibility](docs/compatibility.md) for what each release promises not to break, and [Limitations](docs/limitations.md) before depending on tanGO for anything beyond experimentation.

## What you get

- An explicit app/registry lifecycle (`Config`, `App`, `Registry`) — nothing boots implicitly.
- A URL dispatcher with named routes and reverse lookup, built on [chi](https://github.com/go-chi/chi) internally (chi itself is never part of your imports).
- `Context` helpers for JSON responses, request binding, redirects, and raw request/response escape hatches.
- A model metadata engine that reads plain Go structs and a handful of tags (`tango:"pk"`, `tango:"unique"`, `tango:"index"`, `tango:"fk=Other"`).
- A minimal `db.Store` for CRUD and raw SQL against SQLite or PostgreSQL, with **minimal ORM foundations**: foreign key fields, two-layer validation, and cascade delete — see [relationships and admin foreign keys](docs/guides/relationships-and-admin-foreign-keys.md).
- Generated, typed Go migrations (`tango makemigrations`/`migrate`/`migrate down`) — no separate schema DSL.
- A server-rendered HTML admin (list/create/edit/delete, pagination, sorting, search) behind a session-cookie login, with CSRF protection and rate-limited login attempts — accounts are managed via `tango admin create/resetpassword/deactivate`.
- A CLI (`tango run`/`check`/`makemigrations`/`migrate`/`newproject`/`newapp`/`tui`/`admin`) that wraps ordinary `go build`/`go run` rather than replacing them.

## Principles

> If writing normal Go is simpler than invoking tanGO, write normal Go.

- Prefer Go primitives before replacing them.
- Prefer explicit structure over convention magic.
- Treat JSON and API-only apps as first-class. HTML is optional.
- The admin is a normal app, not a privileged subsystem.
- No custom template language.
- Third-party internals stay behind tanGO's abstractions.

See [ADR 0001](docs/adr/0001-design-principles.md).

## Install

```sh
go get github.com/angvp/tango
```

Install the `tango` CLI (optional, but recommended — it wraps `go run`/`go build` with a few conventions the framework expects your `main.go` to implement):

```sh
go install github.com/angvp/tango/cmd/tango@latest
```

## The smallest runnable application

The fastest way to see a real one is [`examples/jsonapi`](examples/jsonapi): a complete, separate Go module whose one app answers a JSON route. Its `main.go` is written by hand, so it shows every line of a small application.

To start your own project instead:

```sh
tango newproject shop
cd shop
go run .
```

`tango newproject` scaffolds a `go.mod` and a `main.go` pre-wired for SQLite and the admin app, which runs unchanged; run `tango makemigrations`, `tango migrate` and `tango admin create <username>` to create your first admin account, then open `/admin/` (the project prints its address when it starts). `tango newapp greetings` then scaffolds an app stub and prints the line that installs it: adding it to `main.go`'s `InstalledApps` is one line you write yourself — tanGO never edits your `main.go` for you.

`examples/jsonapi`'s app registers one named route:

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

- **[Tutorial](docs/tutorial/01-bootstrap-routing-json.md)** — build one application, a bulletin board, from an empty directory to a deployed container in ten parts: routing and JSON views, persistence and migrations, the HTML admin, foreign keys, server-rendered pages, accounts and forms, API tokens and rate limits, a live WebSocket feed, background jobs and graceful shutdown, and testing and deployment.
- **[Guides](docs/guides/)** — standalone, task-oriented references: project structure, application architecture, configuration, models and tags, routing, `Context`, persistence, migrations, admin, app checks, dialect setup, reusable apps, and relationships/admin foreign keys.
- **[API reference](docs/reference.md)** — the supported public surface, linked to runnable examples. Generated package docs are also available via `go doc` or [pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango) once published.
- **[Examples](examples/)**, each with a README giving its purpose and commands:
  - [`jsonapi`](examples/jsonapi): the smallest JSON application, one hand-written app;
  - [`api-with-admin`](examples/api-with-admin): a JSON API and the HTML admin over one set of models, linked by a foreign key;
  - [`notes-starter`](examples/notes-starter): a keepable starter with generated migrations, the admin and rate limiting;
  - [`jwt-api`](examples/jwt-api): a JSON route behind stateless bearer JWTs;
  - [`realtime-chat`](examples/realtime-chat): WebSocket rooms with JWT auth and graceful shutdown;
  - [`accounts-mail`](examples/accounts-mail): the `accounts` app's password reset and email verification, with emails printed to the terminal;
  - [`reusable-greetings`](examples/reusable-greetings) and [`reusable-greetings-host`](examples/reusable-greetings-host): a reusable app with contributed migrations and static assets, and a host installing it;
  - [`board`](examples/board): the tutorial's finished application, deployable as a container.
- **[Versioning and compatibility](docs/compatibility.md)** — what each release promises not to break, deprecations, and the [changelog](CHANGELOG.md).
- **[Limitations](docs/limitations.md)** — non-goals, security boundaries, and dialect differences.

## Verifying the docs

Documentation in this repository is checked, not just written: every checked-in example under `examples/` is built as its own module in CI, so a stale command or import path in a doc snippet gets caught rather than silently drifting from what actually runs. Run the same check locally before sending a documentation change:

```sh
go test ./...
```

`scripts/check.sh` runs everything CI runs: gofmt, vet, build, the tests and the offline link check (when `lychee` is installed). `scripts/ship.sh` pushes and then waits for CI on the pushed commit; it takes minutes, so run it in the background.

`TestExamplesAreIndependentModulesThatCompile` (at the repository root) builds every `examples/*` module and runs its generated `-check` flag; the migration-generation tests in `internal/cli` similarly `go build` a real generated `migrations` package rather than only inspecting its source text.

## License

MIT — see [LICENSE](LICENSE).

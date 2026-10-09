# Guide: project structure

`tango newproject <name>` produces:

```
<name>/
  .gitignore
  go.mod
  main.go
  migrations/
    migrations.go
  project/
    project.go
  shell/
    main.go
```

- **`go.mod`** — a normal Go module; tanGO is a real dependency, not a vendored framework.
- **`project/project.go`** — `func Config(store *db.Store) tango.Config`, the application's composition and the one place to list your apps. It:
  - loads the `Config` with `tango.LoadConfigFromEnv(tango.WithPortFromEnv())`, so the address is `TANGO_ADDR`, else `PORT`, else `:8000`;
  - installs the admin app;
  - wraps every request, including Unmatched ones (`MiddlewareScopeAll`), in `RequestID`, `Recoverer`, `AccessLogger` and a 1 MiB `MaxBodySize`.

  `Config` does not load `.env` or open a database: each process that calls it does that itself and passes the store in. The server (`main.go`) and the [shell](shell.md) (`shell/main.go`) both call it, so they always register the same apps and models.
- **`main.go`** — pre-wired for SQLite by default: loads `.env` (for `TANGO_DB_DSN`, if you add one yourself), picks the database from `TANGO_DB_DSN`'s scheme with `tango.LoadDBConfigFromEnv` (default `sqlite://app.db`; see [configuration](configuration.md#database-env-helpers)), opens `*sql.DB`, constructs a `db.Store` and builds the config with `project.Config(store)`. Then it:
  - calls `admin.HandleCLI`, `tango.DispatchFlags`, and finally `tango.ServeContext`, under a context that Ctrl-C or `SIGTERM` cancels so the server shuts down gracefully (see [application lifecycle](application-lifecycle.md)). Admin accounts are created afterward with `tango admin create <username>` — see [admin registration](admin-registration.md) — not baked into any generated file. The app-side flag convention is:
  - `-check` — validate app registration and route compilation, then exit (see [app checks](app-checks.md)).
  - `-tango-dump-models` — print registered models as JSON, then exit. Used internally by `tango makemigrations`.
  - `-tango-status` — print registration/database/migration status as JSON, then exit. Used by `tango tui`.
  - `-migrate` / `-migrate -down` — apply or roll back migrations, then exit.
  - `-tango-admin-create` / `-resetpassword` / `-deactivate` — manage Admin accounts (behind `tango admin create/resetpassword/deactivate`), then exit.
  - No flag — start the HTTP server.

  Every one of these is plain Go you can read top to bottom in `main.go` and `project/project.go`; `DispatchFlags` only centralizes the repeated flag behavior. These flag names and their JSON/exit-code expectations are part of the Covered API — see [versioning and compatibility](../compatibility.md#the-covered-api).
- **`shell/main.go`** — the project's own `tango shell` program: it does what `main.go` does up to the store, calls `project.Config(store)`, and hands the result to `shell.Run`. Only this program links the interpreter, so your server binary does not. See the [shell guide](shell.md).
- **`migrations/migrations.go`** — starts as an empty `var Migrations = []migration.Migration{}`. `tango makemigrations` regenerates this file's `Migrations` slice every time it runs, aggregating every migration file in the directory — see the [migrations guide](migrations.md).

Useful scaffold options:

- `tango newproject --dialect=postgres <name>` generates a Postgres-wired project using the `pgx` stdlib driver; its `main.go` (and `shell/main.go`) sets a local-dev `TANGO_DB_DSN` (`postgres://postgres:postgres@localhost:5432/<name>`) when the variable is unset.
- `tango newproject --dialect=sqlite <name>` is the default SQLite shape.
- `tango newproject --no-admin <name>` skips admin wiring entirely.

`tango newapp <name>` adds one directory:

```
apps/<name>/
  app.go
```

`app.go` starts as a stub implementing `Name() string` and `Register(*Registry) error`. tanGO never edits your code for you: wiring a new app into `Config.InstalledApps` is always a line you write by hand, in `project/project.go`. This is deliberate — a project's composition should be fully visible by reading that one file, never inferred from what files happen to exist on disk. `newapp` prints that line when it finishes, with the import path read from your `go.mod`:

```
created apps/posts/app.go
Install it in project/project.go: import "shop/apps/posts" and add posts.App{} to config.InstalledApps.
```

A project made before `tango shell` existed has its apps listed in `main.go` instead; `tango newapp` then says `Install it in main.go`. To add the shell to such a project, see [adding the shell to an existing project](shell.md#adding-the-shell-to-an-existing-project).

As an app grows past a stub, it's a plain Go package: nothing stops you from splitting `app.go` into `models.go`, `app.go`, and `views.go` (as the [`api-with-admin`](../../examples/api-with-admin) example does) once it's more than a screenful. See the [application architecture guide](application-architecture.md) for what comes after that — when to introduce a `services/` layer, and when the app has earned a full domain/ports/adapters split.

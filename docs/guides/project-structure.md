# Guide: project structure

`tango newproject <name>` produces:

```
<name>/
  .gitignore
  go.mod
  main.go
  migrations/
    migrations.go
```

- **`go.mod`** — a normal Go module; tanGO is a real dependency, not a vendored framework.
- **`main.go`** — pre-wired for SQLite by default: loads `.env` (for `TANGO_DB_DSN`/`TANGO_DB_DIALECT`, if you add one yourself), opens `*sql.DB`, constructs a `db.Store`, installs the admin app, then calls `admin.HandleCLI`, `tango.DispatchFlags`, and finally `tango.Serve`. Admin accounts are created afterward with `tango admin create <username>` — see [admin registration](admin-registration.md) — not baked into any generated file. The app-side flag convention is:
  - `-check` — validate app registration and route compilation, then exit (see [app checks](app-checks.md)).
  - `-tango-dump-models` — print registered models as JSON, then exit. Used internally by `tango makemigrations`.
  - `-tango-status` — print registration/database/migration status as JSON, then exit. Used by `tango tui`.
  - `-migrate` / `-migrate -down` — apply or roll back migrations, then exit.
  - `-tango-admin-create` / `-resetpassword` / `-deactivate` — manage Admin accounts (behind `tango admin create/resetpassword/deactivate`), then exit.
  - No flag — start the HTTP server.

  Every one of these is plain Go you can read top to bottom in `main.go`; `DispatchFlags` only centralizes the repeated flag behavior. These flag names and their JSON/exit-code expectations are a stable v0.0.1 contract — see [limitations and compatibility](../limitations.md#stable-v001-cli-app-side-flags).
- **`migrations/migrations.go`** — starts as an empty `var Migrations = []migration.Migration{}`. `tango makemigrations` regenerates this file's `Migrations` slice every time it runs, aggregating every migration file in the directory — see the [migrations guide](migrations.md).

Useful scaffold options:

- `tango newproject --dialect=postgres <name>` generates a Postgres-wired project using the `pgx` stdlib driver and a local-dev default DSN.
- `tango newproject --dialect=sqlite <name>` is the default SQLite shape.
- `tango newproject --no-admin <name>` skips admin wiring entirely.

`tango newapp <name>` adds one directory:

```
apps/<name>/
  app.go
```

`app.go` starts as a stub implementing `Name() string` and `Register(*Registry) error`. tanGO never edits `main.go` for you: wiring a new app into `Config.InstalledApps` is always a line you write by hand. This is deliberate — a project's composition should be fully visible by reading `main.go`, never inferred from what files happen to exist on disk.

As an app grows past a stub, it's a plain Go package: nothing stops you from splitting `app.go` into `models.go`, `app.go`, and `views.go` (as the [`api-with-admin`](../../examples/api-with-admin) example does) once it's more than a screenful. See the [application architecture guide](application-architecture.md) for what comes after that — when to introduce a `services/` layer, and when the app has earned a full domain/ports/adapters split.

# Guide: project structure

`tango newproject <name>` produces:

```
<name>/
  go.mod
  main.go
  migrations/
    migrations.go
```

- **`go.mod`** — a normal Go module; tanGO is a real dependency, not a vendored framework.
- **`main.go`** — pre-wired for SQLite: opens `*sql.DB`, constructs a `db.Store`, and implements the flag-dispatch convention every tanGO app's entry point follows:
  - `-check` — validate app registration and route compilation, then exit (see [app checks](app-checks.md)).
  - `-tango-dump-models` — print registered models as JSON, then exit. Used internally by `tango makemigrations`.
  - `-tango-status` — print registration/database/migration status as JSON, then exit. Used by `tango tui`.
  - `-migrate` / `-migrate -down` — apply or roll back migrations, then exit.
  - No flag — start the HTTP server.

  Every one of these is plain Go you can read top to bottom in `main.go`; there is no hidden dispatch layer.
- **`migrations/migrations.go`** — starts as an empty `var Migrations = []migration.Migration{}`. `tango makemigrations` regenerates this file's `Migrations` slice every time it runs, aggregating every migration file in the directory — see the [migrations guide](migrations.md).

`tango newapp <name>` adds one directory:

```
apps/<name>/
  app.go
```

`app.go` starts as a stub implementing `Name() string` and `Register(*Registry) error`. tanGO never edits `main.go` for you: wiring a new app into `Config.InstalledApps` is always a line you write by hand. This is deliberate — a project's composition should be fully visible by reading `main.go`, never inferred from what files happen to exist on disk.

As an app grows past a stub, it's a plain Go package: nothing stops you from splitting `app.go` into `models.go`, `app.go`, and `views.go` (as the [`api-with-admin`](../../examples/api-with-admin) example does) once it's more than a screenful.

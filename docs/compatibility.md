# Versioning and compatibility

This page says what a tanGO release promises not to break, and how a change that does break something is announced. It applies from v0.1.0 on. The reasoning is in [ADR 0042](adr/0042-v0-1-covers-every-exported-api-except-named-exclusions.md).

## What a version number means

- **Patch releases (`0.1.x`)** never intentionally break the Covered API. Upgrading within a minor version is always safe.
- **Minor releases (`0.x.0`)** may break the Covered API, but only after a Deprecation window (below).
- **Security fixes** are the one exception: a security fix may break the Covered API in any release, and its release notes say so explicitly.

tanGO makes no v1.0 stability promise yet.

## The Covered API

Everything below is covered unless it is named under [what is not covered](#what-is-not-covered).

- **Every exported identifier of every public package**: the root `tango` package, `accounts`, `admin`, `auth`, `auth/jwt`, `db`, `i18n`, `migration`, `model`, `observability`, `ratelimit`, `realtime`, `realtime/websocket` and `testdb`. Exported means types, functions, methods, constants, variables, and the exported fields of exported structs.
- **Every `tango` CLI command** (`newproject`, `newapp`, `run`, `check`, `makemigrations`, `migrate`, `migrate down`, `admin …` and `tui`; `tango help` lists them), with its flags.
- **The app-side flags** a generated `main.go` dispatches, with their JSON output and exit-code expectations:
  - `-check` validates registration, route compilation, and app-contributed checks, then exits non-zero on failure.
  - `-tango-dump-models` prints registered model metadata as JSON for `tango makemigrations`.
  - `-tango-status` prints registration/database/migration status as JSON for `tango tui`; it is read-only and does not create `tango_migrations`.
  - `-migrate` applies pending migrations; `-migrate -down` rolls back the most recently applied one.
  - the `-tango-admin-*` flags (behind `tango admin create`, `resetpassword`, `deactivate`, `grant-staff`, `revoke-staff`, `grant-superuser` and `revoke-superuser`) manage Admin accounts when the admin app is installed.
- **Environment variables**: `TANGO_DB_DSN` (and its `sqlite://`/`postgres://` grammar), `TANGO_ADDR`, `TANGO_TEST_DSN`, and `TANGO_ADMIN_PASSWORD`.
- **The `tango:"…"` struct tag grammar** described in [models and tags](guides/models-and-tags.md).
- **The `tango_migrations` table**: its name and its columns (`app`, `name`, `applied_at`).
- **The generic View-error response**: a View that returns an error, or panics behind `Recoverer`, answers `500` with the JSON body `{"error": "internal error"}`.
- **`accounts`' HTTP endpoints**: `GET`/`POST /accounts/register/`, `GET`/`POST /accounts/login/` and `POST /accounts/logout/`, plus the only two JSON responses `accounts` sends:
  - a rejected or missing CSRF token on a `POST` answers `403` with a JSON object carrying an `"error"` string;
  - rate limiting answers `429` with the same JSON shape and a `Retry-After` header.

  Everything else `accounts` answers is an HTML page and not covered beyond its status: closed registration, for example, is a `403` HTML page, not JSON. A request with a method an endpoint doesn't register gets the router's bare `405`, which isn't part of `accounts`' contract.
- **Observability names**, as listed in the [observability guide](guides/observability.md#stable-events-and-metrics):
  - the log event names `tango.http.view_error`, `tango.http.panic`, `tango.http.access`, `tango.scheduler.job_failed` and `tango.http.request_id_generation_failed`;
  - the metric names `tango_http_request_duration_seconds`, `tango_scheduler_job_invocations_total` and `tango_realtime_room_events_total`;
  - their attribute keys: `route`, `method`, `request_id`, `status`, `duration_seconds`, `error`, `recovered`, `stack`, `job`, `panicked`, `event` and `outcome`;
  - their enumerated values: a scheduler job's `outcome` is `success`, `error`, `canceled` or `panic`; a realtime room event's `event` is `join`, `leave` or `dispatch`, and its `outcome` is `success` or `error`.

A new release may add to any of these: a new function, a new optional flag, a new JSON key, a new attribute. Code and tools that ignore what they don't recognise keep working.

### Build structs with keyed literals

Keyed struct literals are the supported way to build tanGO's structs:

```go
config := tango.Config{InstalledApps: apps, Addr: ":8000"} // supported
config := tango.Config{apps, ":8000", nil}                 // unsupported
```

A minor release may add a field to any covered struct, which breaks an unkeyed literal. This is the rule Go's own compatibility promise uses. `go vet` reports unkeyed literals of imported struct types by default.

## What is not covered

- **`internal/`**. Go already prevents importing it.
- **The migration types and functions that exist for generated files and the CLI**, not for hand-written application code:
  - `migration.Step`, `migration.Column`, and the step structs `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, `DropIndex`, `RenameColumn`, `RenameTable` and `AlterColumnType`;
  - `migration.Model`, `SchemaState`, `TableState` and `ColumnState`;
  - the diff and replay surface: `Diff`, `DiffModels`, `Replay`, `Rename`, `ModelsFromMeta`, `ErrInvalidRename` and `ErrUnsupportedChange`;
  - `migration.ApplyStep`.

  These may change in any minor release, except as the [Generated-file contract](#the-generated-file-contract) protects them. Applications apply migrations with `ApplyPending` and `RollbackLast`, which are covered, as are `Migration`, `EnsureTrackingTable`, `AppliedMigrations`, `IsMissingTrackingTable`, `MigrationKey`, `ErrIrreversibleMigration` and `ErrNoAppliedMigrations`.
- **Best-effort admin extensibility**: `admin.Widget` and the types it uses, the `admin.Options` presentation fields (`Widgets`, `Labels`, `HelpText`, `ReadOnly`, `FieldOrder`), the built-in widgets, and `admin.Branding`/`admin.WithBranding`. Admin's rendering and theme are still settling, so these may change in any minor release without a Deprecation window, though the changelog always lists the change.
- **Admin and `accounts` presentation**: their HTML, templates, CSS, presentation redirects, and admin URLs. A renamed model, for example, changes its admin URL.
- **The wording of error and log messages.** Match errors with `errors.Is` against the exported sentinel errors, which are covered, never against their text.

## Deprecation

A deprecated part of the Covered API is announced in three places:

1. a Go `// Deprecated:` comment on the identifier;
2. a **Deprecated** entry in the [changelog](../CHANGELOG.md);
3. the release notes of the release that deprecates it.

It then keeps working for at least one minor release: something deprecated in 0.2 is removed no earlier than 0.3. A behaviour or flag change with no Go symbol to mark is announced in the changelog one minor release before it lands.

## The Generated-file contract

A migration file written by any released `tango makemigrations` keeps working on every later v0.x release: it compiles, later `tango makemigrations` runs read its `// tango:migration-json` header, and it applies and rolls back. This holds even though the step types it uses are otherwise not covered:

- the fields of `migration.Migration` (`App`, `Name`, `Up`, `Down`, `Reversible`), of `Column`, and of every step struct the generator writes may gain new fields, but a field a released generator wrote is never removed or renamed;
- the header's kinds and keys keep their meaning.

Steps you write by hand, and field combinations the generator never writes, are not protected. tanGO's test suite checks the contract against migration files generated by every released version, on SQLite and PostgreSQL.

## Changes to tanGO's own models

The `admin` and `accounts` models reach your database through your own `tango makemigrations`, so a change to one of them is a change to the Covered API:

- it ships only in a minor release, never a patch;
- the changelog lists it under **Changed**, with instructions to run `tango makemigrations` and apply the result;
- it is always something `tango makemigrations` generates without `--rename` or `--allow-drop`: an added field or a Widening type change;
- renaming or removing one of their fields goes through the Deprecation window: the replacement arrives in one minor release, and the original is removed no earlier than the next.

## How a release is made

Every release has a dated section in the [changelog](../CHANGELOG.md), used as its release notes, and is published only after CI passed on the exact commit being tagged. The maintainer's checklist is [RELEASING.md](../RELEASING.md).

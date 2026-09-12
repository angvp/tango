# Limitations and compatibility

tanGO is pre-v0.1. This page is the honest summary of where it stops, so you can decide whether that's fine for your project before investing in it.

## Non-goals for v0.1

- **No relationships.** Models are described in isolation — no foreign keys, joins, or eager/lazy loading. If your data has real relationships, you model and join them yourself via raw SQL (`Store.Query`/`QueryRow`).
- **No per-app migration story.** Migrations are generated and applied project-wide, against one `migrations/` package and one `tango_migrations` table. Third-party/contributed migrations from installed apps are explicitly parked, not designed for.
- **No rename or type-change migration steps.** A field rename or type change shows up in a generated migration as a drop-and-add, because model metadata doesn't yet track field identity across a rename.
- **`tango shell` is not implemented.** Its direction (a Yaegi-based Go interpreter, not a subprocess-per-command or a debugger) is decided — see [ADR 0005](adr/0005-yaegi-for-tango-shell.md) — but the command itself isn't built yet.
- **No generated documentation site.** Docs are repository Markdown plus generated Go package docs (`go doc`, pkg.go.dev). No Docusaurus/Hugo/mkdocs site for v0.1.
- **App checks aren't enforced automatically.** `Registry.Checks()` aggregates advisory checks apps contribute, but `tango check` doesn't consume them yet — see [app checks](guides/app-checks.md).
- **No `tango newproject`/`newapp` interactive wizard.** Both are non-interactive, single-shot scaffolding commands; there's no guided multi-step prompt flow.

## Security boundaries

- **Admin's Basic Auth is not a full authentication system.** There's no session management, no password hashing/rotation story beyond whatever you pass in, no rate limiting on login attempts, and no CSRF protection on admin's forms. It's suitable for a trusted, low-traffic internal tool behind TLS — not a public-facing admin panel.
- **View errors are never exposed to clients.** A view returning an error always produces a generic 500 JSON body; the real error is only logged server-side. This is a deliberate boundary, not a gap — but it also means you must return your own structured error responses (via `ctx.JSON`) for anything a client legitimately needs to see.
- **No built-in rate limiting or request-size limits** on any route, including admin's create/edit forms. Add your own `net/http` middleware if you need this — tanGO's `Handler()` is a standard `http.Handler` and composes with ordinary middleware.

## Dialect differences

See the [SQLite/PostgreSQL setup guide](guides/sqlite-and-postgresql-setup.md) for the full list. In short: placeholder syntax, `ALTER TABLE` behavior (SQLite rebuilds tables for drop-column/unique-constraint changes; Postgres alters directly), and timestamp column types differ. None of this should be visible in your model or migration code — only in what DDL actually runs, and in raw SQL you write yourself.

## Irreversible migrations

A migration containing `DropColumn` or `DropTable` is marked irreversible. `tango migrate down` on one fails explicitly with a clear error rather than attempting to restore data it has no way to recover. If you need to test a rollback path, do it in a disposable database before applying the same migration to data you care about.

## APIs still expected to change before a stable release

- **`migration.Step`/`Migration`/`Model`'s exact shape.** These back generated code and the diff/replay engine; expect them to evolve as rename/type-change support or a per-app migration story gets designed.
- **`tango check`'s relationship to `Registry.Checks()`.** Automatic enforcement of app-contributed checks is likely, but not yet decided or built.
- **CLI flag surface.** `-check`, `-tango-dump-models`, `-tango-status`, `-migrate[-down]` are stable conventions today, but new flags may be added as new CLI commands need app-side cooperation (`tango tui`'s `-tango-status` is a recent example of this pattern).
- **`Config`'s fields.** `InstalledApps`/`Addr` are the whole of it today; expect this to grow only when a concrete consumer forces the shape, per this project's own design principle.

Everything else documented in the [tutorial](tutorial/01-bootstrap-routing-json.md), [guides](guides/), and [reference](reference.md) reflects real, tested, current behavior in this repository — not a plan.

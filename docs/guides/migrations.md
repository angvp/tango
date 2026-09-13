# Guide: migrations

tanGO's schema migrations are generated Go source, not a separate DSL, and are diffed against a *replayed* state rather than a stored snapshot — there's no schema-snapshot file to go stale relative to the migrations it's meant to summarize.

## Generating

```sh
tango makemigrations
```

This reconstructs "the last known schema" by replaying every existing migration file's typed steps in order (no database connection needed), diffs that against your currently registered models, and writes one file per app with detected changes — sequentially numbered (`0001_auto.go`, `0002_auto.go`, ...). If two apps both have changes in the same run, they get two separate files, never one file bundling both.

Each generated file expresses a small, dialect-agnostic step vocabulary:

- `CreateTable`, `DropTable`
- `AddColumn`, `DropColumn`
- `AlterColumnUnique`
- `CreateIndex`, `DropIndex`

There's no rename step and no column-type-change step — both are indistinguishable from a drop+add given what model metadata currently tracks; a rename shows up as a migration dropping the old column and adding the new one.

A generated file's `var M####Xxx = []migration.Migration{...}` is for human readability of the diff — the file an app's `main.go` actually imports is `migrations/migrations.go`, whose `Migrations` slice is regenerated (aggregating every file) on each `tango makemigrations` run. Never hand-edit `migrations.go`.

## Applying

```sh
tango migrate
```

Runs every not-yet-applied migration's `Up` steps in order, translating each dialect-agnostic step into real DDL for your configured `Dialect` (SQLite uses a table-rebuild pattern for anything it can't `ALTER TABLE` directly, like `DropColumn`; Postgres steps map straight to `ALTER TABLE`/`CREATE INDEX`). Applied migrations are tracked in a `tango_migrations` table — `(app, name, applied_at)`, primary keyed on `(app, name)` — deliberately Django-inspired, like `django_migrations`.

## Rolling back

```sh
tango migrate down
```

Runs the most recently applied migration's `Down` steps and removes its `tango_migrations` row. `Down` steps are generated automatically for reversible operations (`CreateTable`↔`DropTable`, `AddColumn`↔`DropColumn`, `CreateIndex`↔`DropIndex`). A migration containing a lossy step (`DropColumn`, `DropTable`) is marked **irreversible**: `tango migrate down` on it fails explicitly with a clear error rather than attempting to "restore" data it can no longer recover.

## Migration identity

A migration's identity is always the pair `(App, Name)`, never `Name` alone — even though today's sequential numbering happens to make `Name` globally unique in practice. Applied-state tracking, `tango migrate`, and `tango migrate down` all key on the full pair, so two migrations that happened to share a `Name` under different apps would still apply and roll back independently and correctly.

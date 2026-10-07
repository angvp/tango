# Guide: migrations

tanGO's schema migrations are generated Go source, not a separate DSL, and are diffed against a *replayed* state rather than a stored snapshot — there's no schema-snapshot file to go stale relative to the migrations it's meant to summarize.

## Generating

```sh
tango makemigrations
```

This reconstructs "the last known schema" by replaying every existing migration file's typed steps in order (no database connection needed), diffs that against your currently registered models, and writes one file per app with detected changes — sequentially numbered with a UTC timestamp (`0001_auto_20260913124812.go`, `0002_auto_20260913124903.go`, ...). If two apps both have changes in the same run, they get two separate files, never one file bundling both.

Each generated file expresses a small, dialect-agnostic step vocabulary:

- `CreateTable`, `DropTable`
- `AddColumn`, `DropColumn`
- `AlterColumnUnique`
- `CreateIndex`, `DropIndex`

There's no rename step and no column-type-change step — both are indistinguishable from a drop+add given what model metadata currently tracks; a rename shows up as a migration dropping the old column and adding the new one.

A generated file's `var M####Xxx = []migration.Migration{...}` is for human readability of the diff — the file an app's `main.go` actually imports is `migrations/migrations.go`, whose `Migrations` slice is regenerated (aggregating every file) on each `tango makemigrations` run. Never hand-edit `migrations.go`.

## Naming migrations

By default, tanGO combines the next sequence number with `auto` and the current UTC timestamp:

```text
0002_auto_20260913124812.go
```

Use `--name` when a short description will make the migration easier to understand later:

```sh
tango makemigrations --name add-author-indexes
```

That produces `0002_add_author_indexes.go`; the filename and the migration's `Name` are always identical. Names are lowercased, spaces and hyphens become underscores, and repeated or surrounding underscores are cleaned up. Other characters and empty names are rejected. Existing migration files are never overwritten.

## Applying

```sh
tango migrate
```

Runs every not-yet-applied migration's `Up` steps in order, translating each dialect-agnostic step into real DDL for your configured `Dialect` (SQLite uses a table-rebuild pattern for anything it can't `ALTER TABLE` directly, like `DropColumn`; Postgres steps map straight to `ALTER TABLE`/`CREATE INDEX`). Applied migrations are tracked in a `tango_migrations` table — `(app, name, applied_at)`, primary keyed on `(app, name)` — deliberately Django-inspired, like `django_migrations`.

### Order across apps

The order of the migrations slice you pass doesn't matter. Pending migrations run by `Name`, then `App`, with two rules on top:

- Each app's own migrations always run in `Name` order (`0001_…` before `0002_…`).
- A migration that adds a foreign key to another app's table (a `CreateTable` or `AddColumn` column with `References`) runs after that app's migration creating the table — whatever the two apps are called and wherever they sit in `InstalledApps`. PostgreSQL refuses a `REFERENCES` clause naming a table that doesn't exist yet, so this is what lets a cross-app foreign key migrate there at all. It matches how foreign keys already ignore `InstalledApps` order at registration ([ADR 0011](../adr/0011-fk-target-validated-after-registration-not-at-register-time.md)).

If foreign keys between apps form a cycle (app A's initial migration references app B's table and B's references A's), no order works on PostgreSQL. `tango migrate` then fails before applying anything, naming every migration in the cycle. Break the cycle by moving one of the foreign key columns into a later migration of its app, as an `AddColumn`.

## Rolling back

```sh
tango migrate down
```

Runs the most recently applied migration's `Down` steps and removes its `tango_migrations` row. Migrations recorded with the same `applied_at` are rolled back in the reverse of the order they were applied in, so another app's table that a foreign key references is dropped only after the table referencing it. `Down` steps are generated automatically for reversible operations (`CreateTable`↔`DropTable`, `AddColumn`↔`DropColumn`, `CreateIndex`↔`DropIndex`). A migration containing a lossy step (`DropColumn`, `DropTable`) is marked **irreversible**: `tango migrate down` on it fails explicitly with a clear error rather than attempting to "restore" data it can no longer recover.

## Migration identity

A migration's identity is always the pair `(App, Name)`, never `Name` alone. Applied-state tracking, `tango migrate`, and `tango migrate down` all key on the full pair, so two migrations generated with the same explicit name under different apps still apply and roll back independently and correctly.

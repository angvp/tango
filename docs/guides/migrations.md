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

A field or model rename is a step (`RenameColumn`, `RenameTable`), but only when you say so: see [renaming a field or model](#renaming-a-field-or-model). A Widening type change is a step too (`AlterColumnType`): see [changing a field's type](#changing-a-fields-type). A change no step can express — any other type change (`int` to `int64` is the same column type, so it isn't a change), which field is the primary key, or a foreign key's target, including adding or removing `fk=` on an existing field — makes `tango makemigrations` fail with an error naming every such field (`shop.Widget.Stock changes type from integer to boolean, which is not a widening type change`) and write nothing, rather than leave the database silently out of step with your models.

A generated file's `var M####Xxx = []migration.Migration{...}` is for human readability of the diff — the file an app's `main.go` actually imports is `migrations/migrations.go`, whose `Migrations` slice is regenerated (aggregating every file) on each `tango makemigrations` run. Never hand-edit `migrations.go`.

## Renaming a field or model

Model metadata can't tell a renamed field or model from a removed one and a new one, and tanGO never guesses. Tell `makemigrations` with `--rename`, once per rename (Go names or table/column names both work):

```sh
tango makemigrations --rename shop.Widget.Stock=Quantity   # a field
tango makemigrations --rename shop.Widget=Gizmo            # a model
```

A field rename renames the column in place (`ALTER TABLE … RENAME COLUMN`), and a model rename renames its table (`ALTER TABLE … RENAME TO`), on both SQLite and PostgreSQL, so every row is kept. A column's indexes, uniqueness and foreign key come with it; a table's indexes are renamed with it, and every other table's foreign keys point at it under the new name, including other apps' (their migrations that reference the new name run after the rename). Both are reversible: `tango migrate down` renames them back. A renamed field or model needs no `--allow-drop`. Renaming a model also changes its admin URL, which comes from the model's name.

To rename a model and one of its fields in the same run, name the field by the model's **old** name:

```sh
tango makemigrations --rename shop.Widget=Gizmo --rename shop.Widget.Stock=Quantity
```

Every `--rename` is checked against migration history and your models before anything is written:

- the old name must be in history and gone from the models, and the new name must be in the models and not in history;
- no old or new name may appear in two mappings, and a model can't be renamed and also be the new name of another rename;
- a field mapping must name its model by the old name when that model is renamed too;
- a renamed model can't also be given to `--allow-drop`.

A mapping that breaks any of these makes the run fail, naming what you asked for and what history and the models actually have, and nothing is written.

## Changing a field's type

`makemigrations` generates a type change only when every value the column holds converts without loss: a **Widening type change**. These are the only ones:

| From | To | Each value becomes |
|---|---|---|
| `integer` (Go `int`, `int64`, …) | `real` (`float64`) | the same number |
| `integer` | `text` (`string`) | its decimal digits, `42` → `"42"` |
| `real` | `text` | PostgreSQL's float text form on both databases: `1.0` → `"1"`, `1.5` → `"1.5"`, `1e20` → `"1e+20"` |
| `boolean` (`bool`) | `integer` | `true` → `1`, `false` → `0` |
| `boolean` | `text` | `"true"` / `"false"` |

`NULL` stays `NULL`. Every other type change — anything from `text` or `timestamp`, anything narrowing, and any change to a primary key's type — fails `makemigrations` before anything is written; do it by hand in steps you control (add a new field, copy and convert the data, then rename and drop).

The conversion is explicit SQL for each database, never an implicit cast. If the column has a default (from an `AddColumn` with `Default`), the default is converted the same way (`TRUE` becomes `1` for `boolean` to `integer`) and the column keeps it and its `NOT NULL`; a default that isn't a plain literal of the old type makes `makemigrations` refuse, naming the column and the default. A type change never stops halfway: PostgreSQL changes the type and default in one `ALTER TABLE` statement, and SQLite rebuilds the table in one transaction, so on any failure the column, its values and its default are left as they were.

A migration with a type change is **irreversible**, even if it also renames fields: going back would be a narrowing change. Renaming and widening the same field in one run is fine — the rename comes first, then the type change under the new name.

## Dropping a model or field

A migration that drops a model's table or a field's column destroys its data, so `tango makemigrations` never writes one on its own. If your models no longer have a model or field that migration history does, the run fails, lists each one, and writes nothing:

```
tango makemigrations: refusing to drop data no --allow-drop authorises:
  field shop.widget.stock
    renamed? keep its data: --rename shop.widget.stock=<NewField>
to drop them and their data: tango makemigrations --allow-drop shop.widget.stock
```

To drop it, name each model (`app.Model`) or field (`app.Model.Field`) with its own `--allow-drop`; Go names (`shop.Widget.Stock`) and table/column names (`shop.widget.stock`) both work. Every drop needs one, and the run also fails, writing nothing, if an `--allow-drop` names something the change doesn't drop or names the same thing twice. One flag never covers a second, accidental drop. If the same run renames the model, name the dropped field by the model's old name (`--rename shop.Widget=Gizmo --allow-drop shop.Widget.Stock`), as for `--rename`.

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

Runs every not-yet-applied migration's `Up` steps in order, translating each dialect-agnostic step into real DDL for your configured `Dialect` (SQLite drops a column by rebuilding the table in one transaction, keeping its rows, indexes, constraints and incoming foreign keys, and leaving it untouched if the rebuild fails; Postgres steps map straight to `ALTER TABLE`/`CREATE INDEX`). Applied migrations are tracked in a `tango_migrations` table — `(app, name, applied_at)`, primary keyed on `(app, name)` — deliberately Django-inspired, like `django_migrations`.

### Order across apps

The order of the migrations slice you pass doesn't matter. Pending migrations run by `Name`, then `App`, with two rules on top:

- Each app's own migrations always run in `Name` order (`0001_…` before `0002_…`).
- A migration that adds a foreign key to another app's table (a `CreateTable` or `AddColumn` column with `References`) runs after that app's migration creating the table — whatever the two apps are called and wherever they sit in `InstalledApps`. PostgreSQL refuses a `REFERENCES` clause naming a table that doesn't exist yet, so this is what lets a cross-app foreign key migrate there at all. It matches how foreign keys already ignore `InstalledApps` order at registration ([ADR 0011](../adr/0011-fk-target-validated-after-registration-not-at-register-time.md)).

If foreign keys between apps form a cycle (app A's initial migration references app B's table and B's references A's), no order works on PostgreSQL. `tango migrate` then fails before applying anything, naming every migration in the cycle. Break the cycle by moving one of the foreign key columns into a later migration of its app, as an `AddColumn`.

## Rolling back

```sh
tango migrate down
```

Runs the most recently applied migration's `Down` steps and removes its `tango_migrations` row. Migrations recorded with the same `applied_at` are rolled back in the reverse of the order they were applied in, so another app's table that a foreign key references is dropped only after the table referencing it. `Down` steps are generated automatically for reversible operations (`CreateTable`↔`DropTable`, `AddColumn`↔`DropColumn`, `CreateIndex`↔`DropIndex`). A migration containing a lossy step (`DropColumn`, `DropTable`) or a type change (`AlterColumnType`) is marked **irreversible**: `tango migrate down` on it fails explicitly with a clear error rather than attempting to "restore" data it can no longer recover.

## Migration identity

A migration's identity is always the pair `(App, Name)`, never `Name` alone. Applied-state tracking, `tango migrate`, and `tango migrate down` all key on the full pair, so two migrations generated with the same explicit name under different apps still apply and roll back independently and correctly.

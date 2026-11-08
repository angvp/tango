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

A field rename renames the column in place (`ALTER TABLE … RENAME COLUMN`), and a model rename renames its table (`ALTER TABLE … RENAME TO`), on SQLite, PostgreSQL and MySQL, so every row is kept. A column's indexes, uniqueness and foreign key come with it; a table's indexes are renamed with it, and every other table's foreign keys point at it under the new name, including other apps' (their migrations that reference the new name run after the rename). Both are reversible: `tango migrate down` renames them back. A renamed field or model needs no `--allow-drop`. Renaming a model also changes its admin URL, which comes from the model's name.

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
| `real` | `text` | PostgreSQL's float text form on every database: `1.0` → `"1"`, `1.5` → `"1.5"`, `1e20` → `"1e+20"` |
| `boolean` (`bool`) | `integer` | `true` → `1`, `false` → `0` |
| `boolean` | `text` | `"true"` / `"false"` |

A bounded string (`tango:"varchar=n"`) has two more widening changes, and its values never change:

| From | To | Each value becomes |
|---|---|---|
| `varchar(n)` | `text` (the tag removed) | the same string |
| `varchar(n)` | `varchar(m)`, `m > n` | the same string |

`NULL` stays `NULL`. Every other type change — anything from `timestamp`, `text` to anything but a bounded string, and any change to a primary key's type — fails `makemigrations` before anything is written; do it by hand in steps you control (add a new field, copy and convert the data, then rename and drop).

### Narrowing a string

Adding `varchar=n` to a bare string, or lowering `n`, is a **narrowing**: it can only work if every value already fits. `makemigrations` generates it as an `AlterColumnType`, and applying it first checks the data in Go, before any DDL. It reads each row's primary key and value, counts runes exactly as `Store.Create` does (never the database's own `length()`, so every dialect agrees), and, if any value is too long, fails the migration with the number of rows and one offending key:

```text
tango migration: headline.title: cannot narrow text to varchar(5): 2 rows hold a longer value (lengths count characters), for example the row with id = 2; shorten or clean that data first, then run the migration again
```

Every narrowing in a migration is checked before the migration's first step runs (a rename in the same migration is checked under the names the database holds now), so nothing is truncated or rewritten, the schema and the data stay as they were, and the migration is not recorded as applied. Shorten or clean the data, then run `tango migrate` again. The check finds the table's own primary-key column, whatever it is called and whatever its type. It needs exactly one primary-key column to name the offending row: a table with a composite primary key (possible only through hand-written migrations, since models have a single `pk` field) cannot be narrowed by a migration, and the step fails with an error saying so rather than checking without a key. Narrow such a table by hand in a migration you control.

The conversion is explicit SQL for each database, never an implicit cast. If the column has a default (from an `AddColumn` with `Default`), the default is converted the same way (`TRUE` becomes `1` for `boolean` to `integer`) and the column keeps it and its `NOT NULL`; a default that isn't a plain literal of the old type makes `makemigrations` refuse, naming the column and the default. A type change never stops halfway: PostgreSQL changes the type and default in one `ALTER TABLE` statement, MySQL cannot roll DDL back so it checks the data first and converts through a staging step, and SQLite rebuilds the table in one transaction, so on any failure the column, its values and its default are left as they were.

A migration with a widening type change is **irreversible**, even if it also renames fields: going back would be a narrowing change. (A migration made only of string narrowings is the exception, below.) Renaming and widening the same field in one run is fine — the rename comes first, then the type change under the new name.

Reversibility is decided per migration, and only length changes of a string can reverse. A migration whose type changes are all narrowings (`text` to `varchar(n)`, or a smaller `varchar`) is reversible: its `Down` widens each column again, which is always safe. A migration with any widening step (`varchar` to `text`, a larger `varchar`, or any change in the table above) is irreversible as a whole and has no `Down`, even when it also holds a narrowing. Rolling one back is refused, because values longer than the old limit may have been written since; to go back, change the tag and run `makemigrations`, which generates an explicit narrowing that checks the data first.

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

That produces `0002_add_author_indexes.go`, named after the migration's `Name`. `Validate` checks `Name`, not the file name. Names are lowercased, spaces and hyphens become underscores, and repeated or surrounding underscores are cleaned up. Other characters and empty names are rejected. Existing migration files are never overwritten.

## Migration numbers and conflicts

Within one app, a migration's name starts with a four-digit number followed by an underscore (`^\d{4}_`), as in `0005_add_email`. The registry is checked against these rules:

- An app may not register the same `(App, Name)` twice.
- Within one app, two different names may not share a number. `0005_add_email` and `0005_add_phone` conflict.
- The same number in two different apps is valid. Each app keeps its own sequence.
- Names without the four-digit prefix are not checked for a number, but an exact duplicate of one still fails.

The check reads the registry, not the database. It rejects a conflict even when one of the two migrations is already applied, so it fires in development and CI, not only in production. Every conflict is reported in one error, in a fixed order. A registry with two conflicts looks like this:

```text
tango migration: conflicting migration names:
- app "blog": number 0005 is shared by "0005_add_email", "0005_add_phone"; renumber one migration
- app "blog": name "0007_x" is registered 2 times; remove the duplicate registration or rename one migration
```

The check runs in each of these places, and each one refuses without changing anything:

| Entry point | Behaviour |
|---|---|
| `-migrate`, `migration.ApplyPending` | Refuses before any database access. No `tango_migrations` table is created, and nothing is read or applied. |
| `-check` (`tango check`) | Fails with `check failed: ...` and the conflict text. A configuration failure found in the same run is reported with it. |
| `-tango-status`, `tango tui` | Reports the conflict in `migrationError`, and shows it on the migrations line as `migrations: CONFLICT (...)`. Status still works. While `migrationError` is set, the TUI does not offer "Apply pending migrations", and shows the conflict as the reason. `-migrate` still refuses, as the safety net. |
| `tango makemigrations` | Exits 1 with `tango makemigrations: ...` and writes no files. |
| `-tango-dump-models` | Fails with `check failed: ...` and the conflict text before any model is printed, whatever other flag is given. |

`-migrate -down` (`migration.RollbackLast`) is the one exception and is not listed above: it does not refuse a conflicting registry. See [rolling back](#rolling-back) below.

There is no bypass: no flag, environment variable or configuration option skips the check. The remedy is to renumber one of the migrations.

### Renumbering after a merge

When two branches each add a migration, the merged registry contains a conflict. To resolve it:

1. Read the error. It names the app, the shared number or the duplicated name, and every migration involved.
2. Choose one of the conflicting migrations, rename its file to the next free number in that app, and set its `Name` to the same value. `Validate` checks `Name`, and `makemigrations` names new files after it.
3. Run `tango makemigrations` so that `migrations/migrations.go` is regenerated to match the files on disk. Do not edit `migrations.go` by hand.
4. Run `tango check` until it passes, then `tango migrate`.

Renumber only a migration that no database has applied. Applied state is keyed on `(App, Name)`, so renaming an applied migration makes it look unapplied, and `tango migrate` runs it again. If both migrations have been applied somewhere, the database already holds both; resolve that with a new migration, not a rename.

A duplicated registration of the same `(App, Name)` is usually a file included twice, or a migration copied without changing its name. Remove the extra registration, or rename one migration if the two are genuinely different.

### Order and the tracking table

`tango_migrations` has no sequence column. It records only `(app, name, applied_at)`, so the order in which migrations were applied is not stored as a sequence. Rows that share an `applied_at` are ordered by the registry's apply order when rolling back, as described under [rolling back](#rolling-back). Adding a sequence column would change the tracking schema and needs an upgrade path for existing databases, so it is not part of this rule.

## Applying

```sh
tango migrate
```

Runs every not-yet-applied migration's `Up` steps in order, translating each dialect-agnostic step into real DDL for your configured `Dialect` (SQLite drops a column by rebuilding the table in one transaction, keeping its rows, indexes, constraints and incoming foreign keys, and leaving it untouched if the rebuild fails; Postgres and MySQL steps map straight to `ALTER TABLE`/`CREATE INDEX`). On MySQL each DDL statement commits as it runs, so a migration that fails part-way cannot roll back: the error names the failed step and every step already applied, no tracking row is written, and the migration never resumes by itself. Repair the schema by hand, then run it again. Applied migrations are tracked in a `tango_migrations` table — `(app, name, applied_at)`, primary keyed on `(app, name)`.

### Order across apps

The order of the migrations slice you pass doesn't matter. Pending migrations run by `Name`, then `App`, with two rules on top:

- Each app's own migrations always run in `Name` order (`0001_…` before `0002_…`).
- A migration that adds a foreign key to another app's table (a `CreateTable` or `AddColumn` column with `References`) runs after that app's migration creating the table — whatever the two apps are called and wherever they sit in `InstalledApps`. PostgreSQL and MySQL refuse a `REFERENCES` clause naming a table that doesn't exist yet, so this is what lets a cross-app foreign key migrate there at all. It matches how foreign keys already ignore `InstalledApps` order at registration.

If foreign keys between apps form a cycle (app A's initial migration references app B's table and B's references A's), no order works on PostgreSQL or MySQL. `tango migrate` then fails before applying anything, naming every migration in the cycle. Break the cycle by moving one of the foreign key columns into a later migration of its app, as an `AddColumn`.

## Rolling back

```sh
tango migrate down
```

Runs the most recently applied migration's `Down` steps and removes its `tango_migrations` row. Migrations recorded with the same `applied_at` are rolled back in the reverse of the order they were applied in, so another app's table that a foreign key references is dropped only after the table referencing it.

Rollback is exempt from the registry check above, because it is the recovery path. It runs against a registry with a conflict, and on a fresh database it creates the `tango_migrations` table if that table is missing, then fails with `no applied migrations to roll back`. Nothing else is written. It has two guards of its own. Both run before any `Down` step, so a refusal changes nothing:

- **Unorderable tie (`ErrUnorderableTie`).** Several rows share the latest `applied_at` and cannot be ordered. That happens when a tied migration is missing from the registry, or when the tied migrations' cross-app foreign keys form a cycle. The error names the tied rows and their shared `applied_at`. A tie that can be ordered rolls back as before.
- **Ambiguous target (`ErrAmbiguousRollbackTarget`).** The `(App, Name)` to roll back is registered more than once, so the registry does not say which `Down` steps belong to it. Remove the extra registration, as described under [migration numbers and conflicts](#migration-numbers-and-conflicts).

A repeated number with different names does not block rollback. The identity that matters here is `(App, Name)`. `Down` steps are generated automatically for reversible operations (`CreateTable`↔`DropTable`, `AddColumn`↔`DropColumn`, `CreateIndex`↔`DropIndex`). A migration containing a lossy step (`DropColumn`, `DropTable`) or a widening type change (`AlterColumnType`) is marked **irreversible**: `tango migrate down` on it fails explicitly with a clear error rather than attempting to "restore" data it can no longer recover.

## Migration identity

A migration's identity is always the pair `(App, Name)`, never `Name` alone. Applied-state tracking, `tango migrate`, and `tango migrate down` all key on the full pair, so two migrations generated with the same explicit name under different apps still apply and roll back independently and correctly.

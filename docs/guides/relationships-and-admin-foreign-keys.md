# Guide: relationships and admin foreign keys

tanGO supports exactly one relationship shape in v0.1: a many-to-one **foreign key field**. No many-to-many, no reverse accessors, no automatic joins — see [limitations](../limitations.md) for the full boundary.

Together with the validation and cascade-delete behavior below, this is what tanGO calls its **minimal ORM foundations**: a small, deliberately bounded set of relationship-aware behavior across `model`, `db`, and `admin`. It is not a full ORM — there's no `QuerySet`, no lazy loading, no many-to-many, no reverse managers, no signals, no nested writes. See [where `Store` stops](#where-store-stops-and-raw-sql-begins) below for the boundary this implies.

## Declaring a foreign key

A foreign key field is named after the column it stores, with an `ID` suffix — the same convention as `ID int64 \`tango:"pk"\`` — tagged with the target model's name:

```go
type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID       int64  `tango:"pk"`
	Title    string
	AuthorID int64  `tango:"fk=Author"`
}
```

`Author` doesn't need to be registered before `Post` — the target's existence is checked once, after every installed app finishes registering (not at the moment `Post` itself registers), so `InstalledApps` order never constrains which app can declare a foreign key relative to the app that registers its target.

## Two validation layers

tanGO checks foreign keys at two different times, for two different things:

- **Schema validation** — `Registry.Models().ValidateForeignKeys()` (called automatically by `Config.Check`, and so by `-check`) confirms that the *related model itself* is registered. It runs once, after every installed app finishes registering. Failure returns `model.ErrUnknownForeignKeyTarget`.
- **Referential-integrity validation** — `Store.Create` and `Store.Update` confirm that a set foreign key field's value actually references an *existing row* of the related model, via a preflight `SELECT` before the write. Failure returns `db.ErrInvalidForeignKey`. A foreign key field left at its Go zero value (`0`) is treated as unset and skipped — tanGO has no nullable-field mechanism, so this is a convention that relies on primary keys starting at `1` in practice, not a general "optional foreign key" feature.

Referential-integrity validation only runs when `Store.UseModels` has been called (the same gate that enables cascade delete, below), and is deliberately not wrapped in the same transaction as the write it guards — the generated DB-level `REFERENCES` constraint (see above) is what actually closes the narrow race this leaves open, when it's enabled.

## Naming collisions

Like every model name in tanGO, `fk=Author` refers to a bare, unnamespaced model name — if two reusable apps both register a model called `Author`, `fk=Author` is ambiguous in exactly the way described in [reusable apps](reusable-apps.md)'s naming-discipline section. Give models distinct names if you expect to install apps you don't control the naming of.

## Migrations and the generated constraint

`tango makemigrations` generates a real DB-level `REFERENCES` constraint for a foreign key field — `RESTRICT`/`NO ACTION`, never `CASCADE` at the database level (see below for why). On SQLite, this constraint is only enforced if `PRAGMA foreign_keys=ON` has been set on the connection; `db.SQLiteForeignKeysDSN(dsn)` appends the driver's `_foreign_keys=on` DSN parameter for you, and `tango newproject --dialect=sqlite`'s generated scaffold already calls it. If you open your own SQLite connection outside the scaffold and want the constraint enforced, call it yourself:

```go
sqlDB, err := sql.Open("sqlite", db.SQLiteForeignKeysDSN(dsn))
```

Postgres enforces the constraint natively with no extra step.

## Cascade delete

Deleting a row that other rows reference through a foreign key field **cascades**: `Store.Delete` finds every row across every registered model that points at the one being deleted, deletes them first (recursively — a chain of references is fully unwound), then deletes the target — all inside one transaction, with a cycle guard for circular or self-referential foreign keys. This is Django-inspired — Django's ORM implements `on_delete=CASCADE` the same way, in the ORM's own delete collector, not via the database — and is likewise not a database feature here: the generated DB constraint stays plain `RESTRICT`, and only ever matters as a backstop against raw SQL or another tool deleting a referenced row directly, bypassing `Store.Delete`.

This is automatic for any app using the standard `Registry`/`Serve` flow — `Registry.SetStore` wires the model registry into the store for you. A `*db.Store` constructed and used entirely on its own (outside a `tango.Registry`) needs one explicit call to opt in:

```go
store := db.NewStore(sqlDB, dialect)
store.UseModels(modelRegistry) // enables cascade delete and referential-integrity validation; omit it and Delete only removes the target row, and Create/Update skip the row-level foreign key check
```

## Admin: the FK select and related labels

An `fk=`-tagged field renders as a `<select>` in the admin create/edit form, populated from every row of the related model, instead of a plain numeric input. What each option shows is controlled by `admin.Options.Label` on the *related* model's own registration — not the model with the foreign key:

```go
registry.Admin().Register(Author{}, admin.Options{
	Label: "Name", // shown wherever an Author is referenced elsewhere
})
```

The same `Label` value is used for the related-object column on any list page that includes the FK field in `ListDisplay`. If the related model has no `Label` configured — or isn't admin-registered at all — every FK display falls back to the raw primary key value; this is never a hard error, so adding a foreign key never breaks an existing admin registration that hasn't gotten around to setting `Label` yet.

Building the select and the related labels costs one query per related row shown (accepted for v0.1 — see [limitations](../limitations.md)).

## Where `Store` stops and raw SQL begins

`Store` handles single-model CRUD plus one-hop foreign key integrity: schema validation, referential-integrity validation, and cascade delete, all described above. That's the whole of tanGO's minimal ORM foundations, by design — not a gap waiting to be filled. Anything that needs a join, an aggregate, or filtering across models is raw SQL via `Store.Query`/`QueryRow`, same as it is for every other kind of query tanGO doesn't have a typed helper for.

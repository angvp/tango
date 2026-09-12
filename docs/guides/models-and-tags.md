# Guide: models and tags

Models are plain Go structs — no base class, no embedding, no code generation:

```go
type Post struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique"`
	Body      string
	CreatedAt time.Time `tango:"index"`
}
```

Register one with `registry.Models().Register(Post{})` inside your app's `Register` method.

## Tags

The `tango` struct tag takes a comma-separated list of options:

| Tag | Meaning |
|---|---|
| `pk` | This field is the primary key. Exactly one field must be tagged `pk` — zero or more than one fails `Register` with a clear error. There is no `ID`-name auto-detection: tanGO never guesses. |
| `unique` | A unique constraint. Enforced at the database level once a migration adds it (see the [migrations guide](migrations.md)'s `AlterColumnUnique` step). |
| `index` | A database index, created via the `CreateIndex` migration step. |

Combine them: `tango:"pk,unique"` is valid, though redundant (a primary key is already unique).

## Supported field kinds

- `string`, `bool`
- All signed and unsigned integer kinds (`int`, `int8`...`int64`, `uint`...`uint64`)
- `float32`, `float64`
- `time.Time`

Anything else — including an embedded/anonymous struct field — fails `Register` with a clear error identifying the offending field. There is no relationship support (foreign keys, joins) in v0.1; see [limitations](../limitations.md).

## Naming

`ModelMeta.Name` is the Go type's name exactly as declared (`"Post"`, not `"post"`) — this is the key you pass to `registry.Models().Get(name)` and the value that appears in `tango_migrations.app`-adjacent bookkeeping. Table and column names are a separate, automatic derivation: `db.ColumnName` lowercases and snake_cases both (`Post` → `post` table, `CreatedAt` → `created_at` column). You never see or configure this mapping directly — it's applied consistently by `db.Store` and the migration generator.

## Model ownership (`ModelMeta.App`)

A model is recorded as owned by whichever app's `Register` was running when `Models().Register` was called — set automatically by `Registry.RunRegistration` before each app's `Register` runs. This is what lets `tango makemigrations` group changes by app and write one migration file per app; you never set it yourself.

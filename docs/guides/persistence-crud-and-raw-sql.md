# Guide: database CRUD and raw SQL

`db.Store` is tanGO's persistence boundary — metadata-driven CRUD for the common case, with raw SQL always available as an escape hatch. It is not a full ORM: no lazy loading and no query builder DSL; relationship-aware behavior is deliberately limited to foreign-key integrity, cascade delete, and admin support.

```go
store := db.NewStore(sqlDB, db.SQLite) // or db.Postgres
```

## CRUD

Every method takes the model's `model.ModelMeta` (from `registry.Models().Get("Post")`) and a destination value:

- **`Create(ctx, meta, dest any) error`** — inserts `dest`, backfilling a database-generated primary key into it.
- **`Get(ctx, meta, pk any, dest any) error`** — scans the row matching `pk` into `dest`. Returns `db.ErrNotFound` (wrapped) if no row matches.
- **`List(ctx, meta, query db.Query, dest *[]T) error`** — scans every matching row into `dest`. `db.Query{Limit, Offset, OrderBy}` carries pagination and ordering; `OrderBy` entries are Go field names, optionally prefixed with `-` for descending (`"-CreatedAt"`), and are validated against the model's actual fields — an unknown field fails with a clear error rather than silently doing nothing.
- **`Update(ctx, meta, dest any) error`** — writes `dest`'s current field values to the row matching its primary key. Returns `db.ErrNotFound` if no row matches.
- **`Delete(ctx, meta, pk any) error`** — deletes the row matching `pk`. Returns `db.ErrNotFound` if no row matches.

Table and column names are derived automatically (snake_case of the Go type/field names) — you never configure this mapping.

## Raw SQL

When the CRUD surface isn't enough, `Store` exposes two raw-SQL methods that reuse its scan-into-`dest` machinery:

- **`QueryRow(ctx, dest any, sql string, args ...any) error`** — one row into `dest`.
- **`Query(ctx, dest *[]T, sql string, args ...any) error`** — every row appended into `dest`, matching columns to struct fields by name (case-insensitively).

A returned column matches a destination field if it case-insensitively equals *either* the Go field name directly (`title` matches `Title`) *or* `db.ColumnName(field.Name)` — the same snake_case derivation `Store`'s own generated schema uses (`author_id` matches `AuthorID`). This means a raw query selecting tanGO's own generated column names scans straight into the matching Go-named struct with no aliasing required:

```go
var books []struct {
	AuthorID int64
	Title    string
}
err := store.Query(ctx, &books, "SELECT author_id, title FROM book WHERE author_id = ?", authorID)
```

An `AS` alias is still how you name a column that isn't itself a real column — a computed/aggregate value (`COUNT(*) AS count`) or a joined table's column you want under a different destination field name:

```go
var result struct {
	Count int
}
err := store.QueryRow(ctx, &result, "SELECT COUNT(*) AS count FROM post WHERE title LIKE ?", "%tango%")
```

`sql` must already use the placeholder syntax matching the `Store`'s configured `Dialect` (`?` for SQLite, `$1`/`$2`/... for Postgres) — neither method translates or validates placeholder syntax between dialects. See the [SQLite/PostgreSQL setup guide](sqlite-and-postgresql-setup.md) for where that dialect is selected.

## Errors

`db.ErrNotFound` is the sentinel for "no matching row" on `Get`/`Update`/`Delete`; check it with `errors.Is`.

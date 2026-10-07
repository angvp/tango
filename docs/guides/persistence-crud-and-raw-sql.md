# Guide: database CRUD and raw SQL

`db.Store` is tanGO's persistence boundary — metadata-driven CRUD for the common case, with raw SQL always available as an escape hatch. It is not a full ORM: no lazy loading and no query builder DSL; relationship-aware behavior is deliberately limited to foreign-key integrity, cascade delete, and admin support.

```go
store := db.NewStore(sqlDB, db.SQLite) // or db.Postgres
```

## CRUD

Every method takes the model's `model.ModelMeta` (from `registry.Models().Get("Post")`) and a destination value:

- **`Create(ctx, meta, dest any) error`** — inserts `dest`, backfilling a database-generated primary key into it.
- **`Get(ctx, meta, pk any, dest any) error`** — scans the row matching `pk` into `dest`. Returns `db.ErrNotFound` (wrapped) if no row matches.
- **`List(ctx, meta, query db.Query, dest *[]T) error`** — scans every matching row into `dest`. `db.Query{Where, Any, Limit, Offset, OrderBy}` carries filtering, pagination, and ordering; `OrderBy` entries are Go field names, optionally prefixed with `-` for descending (`"-CreatedAt"`), and are validated against the model's actual fields — an unknown field fails with a clear error rather than silently doing nothing.
- **`Update(ctx, meta, dest any) error`** — writes `dest`'s current field values to the row matching its primary key. Returns `db.ErrNotFound` if no row matches.
- **`Delete(ctx, meta, pk any) error`** — deletes the row matching `pk`. Returns `db.ErrNotFound` if no row matches.

Table and column names are derived automatically (snake_case of the Go type/field names) — you never configure this mapping.

### Explicit primary keys

`Create` with the primary key already set (`store.Create(ctx, meta, &Post{ID: 100, ...})`) inserts that ID instead of letting the database pick one. A later `Create` without an ID still gets a fresh one past it, on both dialects: SQLite's `AUTOINCREMENT` does this by itself, and on PostgreSQL `Create` advances the table's `BIGSERIAL` sequence to the supplied ID in the same transaction as the insert. It never moves the sequence backwards, so inserting a lower ID than the sequence has already handed out changes nothing. That transaction briefly locks the table against other inserts; a `Create` without an ID takes no extra lock or statement.

Rows you insert with raw SQL (through your own `*sql.DB`) bypass this. On PostgreSQL, if your raw `INSERT` supplies an ID, keeping the sequence ahead of it is your job, for example `SELECT setval(pg_get_serial_sequence('"post"', 'id'), (SELECT MAX(id) FROM "post"))` after a bulk load. Otherwise the next `Create` without an ID can fail with a duplicate-key error.

## Filtering with `Where`

`Where` is a slice of `db.Condition{Field, Op, Value}`. Each `Field` is a Go model field name; conditions are joined with AND. For example, given a `Post` model with a `CreatedAt time.Time` field:

```go
var posts []Post
err := store.List(ctx, meta, db.Query{
	Where: []db.Condition{
		{Field: "CreatedAt", Op: db.OpGte, Value: start},
		{Field: "CreatedAt", Op: db.OpLt, Value: end},
	},
	OrderBy: []string{"CreatedAt"},
}, &posts)
```

The example returns posts in the `[start, end)` time range. `examples/api-with-admin/apps/posts/views.go` shows the same API in a working app view: an `author_id` query param builds a `Where` condition, and a `q` query param builds an `Any` group (`OpLike` across `Title`/`Body`) — combined with AND when both are present.

| Operator | Meaning | Allowed fields |
| --- | --- | --- |
| `db.OpEq`, `db.OpNe` | Equal, not equal | Any supported model field |
| `db.OpGt`, `db.OpGte`, `db.OpLt`, `db.OpLte` | Greater/less than, inclusive variants | Numeric fields and `time.Time` |
| `db.OpLike` | SQL `LIKE` pattern match | String fields |

`Value` must have the field's Go kind (`time.Time` for a time field). `nil` is rejected; use literal `false`, `0`, or `""` to filter for those values. Nil and empty `Where` slices both mean no filter. Unknown fields, unsupported operators, incompatible field kinds, and mismatched value types return errors before SQL execution. Values are passed as SQL parameters on both SQLite and Postgres.

`Any` is one OR group, combined with `Where` using AND. For example, `Where: []db.Condition{{Field: "Active", Op: db.OpEq, Value: true}}` and `Any: []db.Condition{{Field: "Name", Op: db.OpLike, Value: "Al%"}, {Field: "Name", Op: db.OpLike, Value: "Be%"}}` selects active rows whose name matches either pattern. `OpLike` accepts an SQL pattern: `%` and `_` are wildcards. Case matching follows the configured database's `LIKE` behavior, which can differ between SQLite and Postgres. An empty `Any` applies no OR filter.

`Store.Count(ctx, meta, query)` returns the total rows matching `Where` and `Any`. It ignores `Limit`, `Offset`, and `OrderBy`, so the same `Query` can drive a paginated `List` and its total count.

This API does not support nested boolean groups, automatic wildcard escaping, `IS NULL`, joins, cross-model filtering, or filtering `Store.Get`. Use raw SQL below for queries outside this scope.

## NULL and zero values

tanGO fields are plain Go values, never pointers or `sql.Null*` wrappers, so `NULL` and a field's zero value mean the same thing. Every read (`Get`, `List`, `Query`, `QueryRow`) turns a `NULL` column into the field's zero value: `""`, `0`, `false`, or the zero `time.Time`. That's what happens to existing rows when a migration adds a column: they read as the zero value until you write something else. A raw query's `LEFT JOIN` with no match reads the same way. Going the other way, a foreign key field left at `0` is written as `NULL` (see [relationships](relationships-and-admin-foreign-keys.md)); other fields are written as their value.

A `Where` condition compares with `=`, and `NULL` never equals anything, so filtering on a zero value doesn't match `NULL` rows. Use raw SQL with `IS NULL` for that. A raw-SQL destination that needs to tell `NULL` from zero can use a pointer or `sql.Null*` field; those keep their usual meaning. See [ADR 0038](../adr/0038-null-reads-as-the-zero-value-and-unset-foreign-keys-write-null.md).

## Raw SQL

When the CRUD surface isn't enough, `Store` exposes two raw-SQL methods that reuse its scan-into-`dest` machinery:

- **`QueryRow(ctx, dest any, sql string, args ...any) error`** — one row into `dest`.
- **`Query(ctx, dest *[]T, sql string, args ...any) error`** — every row appended into `dest`, matching columns to struct fields by name (case-insensitively).

Raw SQL is yours: `Store` doesn't rewrite it, so use your dialect's placeholders and quote any table or column named after a reserved word yourself (`SELECT "order" FROM "user"` on PostgreSQL, `` SELECT `order` FROM `user` `` on SQLite). `Store`'s own generated SQL already quotes every identifier.

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

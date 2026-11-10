# Guide: database CRUD and raw SQL

`db.Store` is tanGO's persistence boundary — metadata-driven CRUD for the common case, with raw SQL always available as an escape hatch. It is not a full ORM: no lazy loading and no query builder DSL; relationship-aware behavior is deliberately limited to foreign-key integrity, cascade delete, explicit loading of related rows with `Query.Preload`, `Store.Related` and `Store.RelatedMany`, and admin support. See [loading related rows](relationships-and-admin-foreign-keys.md#loading-related-rows).

```go
store := db.NewStore(sqlDB, db.SQLite) // or db.Postgres, db.MySQL
```

## CRUD

Every method takes the model's `model.ModelMeta` (from `registry.Models().Get("Post")`) and a destination value:

- **`Create(ctx, meta, dest any) error`** — inserts `dest`, backfilling a database-generated primary key into it.
- **`Get(ctx, meta, pk any, dest any) error`** — scans the row matching `pk` into `dest`. Returns `db.ErrNotFound` (wrapped) if no row matches.
- **`List(ctx, meta, query db.Query, dest *[]T) error`** — scans every matching row into `dest`. `db.Query{Where, Any, Filter, Limit, Offset, OrderBy}` carries filtering, pagination, and ordering; `OrderBy` entries are Go field names, optionally prefixed with `-` for descending (`"-CreatedAt"`), and are validated against the model's actual fields — an unknown field fails with a clear error rather than silently doing nothing.
- **`First(ctx, meta, query db.Query, dest *T) error`** — the first matching row into `dest`, a pointer to one model struct. With no `OrderBy` it orders by the primary key, so the answer is deterministic; `Offset` is honoured, `Limit` is ignored. No match returns `db.ErrNotFound` (wrapped, naming the model), and `dest` is left untouched on any error.
- **`Exists(ctx, meta, query db.Query) (bool, error)`** — whether any row matches; `Limit`, `Offset` and `OrderBy` are ignored.
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

The example returns posts in the `[start, end)` time range. If `start` or `end` can be the zero time, check it with `IsZero` first: a zero time is not a query value, and the query returns `ErrInvalidQuery` (see the value rule below). `examples/api-with-admin/apps/posts/views.go` shows the same API in a working app view: an `author_id` query param builds a `Where` condition, and a `q` query param builds an `Any` group (`OpLike` across `Title`/`Body`) — combined with AND when both are present.

| Operator | Meaning | Allowed fields |
| --- | --- | --- |
| `db.OpEq`, `db.OpNe` | Equal, not equal | Any supported model field |
| `db.OpGt`, `db.OpGte`, `db.OpLt`, `db.OpLte` | Greater/less than, inclusive variants | Numeric fields, `time.Time` and `model.Date` |
| `db.OpLike` | SQL `LIKE` pattern match | String fields |

`Value` must have the field's Go kind (`time.Time` for a time field, `model.Date` for a date field). `nil` is rejected; use literal `false`, `0`, or `""` to filter for those values. A zero `time.Time` or zero `model.Date` is not a query value: `=`, `!=`, the four range operators and every `IN` element with one return `ErrInvalidQuery` before any SQL runs, and the error names `db.IsNull` and `db.IsNotNull`, which find unset rows. Nil and empty `Where` slices both mean no filter. Unknown fields, unsupported operators, incompatible field kinds, and mismatched value types return errors before SQL execution. Values are passed as SQL parameters on both SQLite and Postgres.

`Any` is one OR group, combined with `Where` using AND. For example, `Where: []db.Condition{{Field: "Active", Op: db.OpEq, Value: true}}` and `Any: []db.Condition{{Field: "Name", Op: db.OpLike, Value: "Al%"}, {Field: "Name", Op: db.OpLike, Value: "Be%"}}` selects active rows whose name matches either pattern. `OpLike` accepts an SQL pattern: `%` and `_` are wildcards. Case matching follows the configured database's `LIKE` behavior, which can differ between SQLite and Postgres. An empty `Any` applies no OR filter.

`Store.Count(ctx, meta, query)` returns the total rows matching `Where`, `Any` and `Filter`. It ignores `Limit`, `Offset`, and `OrderBy`, so the same `Query` can drive a paginated `List` and its total count.

### Grouped filters, `IN` and `NULL`

`Query.Filter` is an optional boolean tree, ANDed with `Where` and `Any`. Build it with `db.Cond`, `db.And`, `db.Or`, `db.Not`, `db.In`, `db.IsNull` and `db.IsNotNull`:

```go
published := db.And(
	db.Or(db.Cond("Status", db.OpEq, "published"), db.Cond("Featured", db.OpEq, true)),
	db.Cond("OwnerID", db.OpEq, ownerID),
)
err := store.List(ctx, meta, db.Query{Filter: &published, OrderBy: []string{"-CreatedAt"}}, &posts)
```

- **`IN`.** `db.In("ID", []int64{3, 5, 8})` takes any slice or array whose elements have the field's type (`[]any` is accepted when every element does). A nil or empty list matches nothing, whatever the dialect. A list holds at most `db.MaxInValues` (1,000) values, and one query sends at most `db.MaxQueryArgs` (10,000) bind values across `Where`, `Any` and `Filter`. A zero `time.Time` or zero `model.Date` in a list is rejected. `IN` follows the same literal SQL as `=`: a zero foreign key is stored as `NULL`, so `OwnerID IN (0, 3)` does not match unset rows (it matches only owner 3), and it is not rejected. For both assigned IDs and unset rows, say so explicitly: `db.Or(db.In("OwnerID", []int64{3, 5}), db.IsNull("OwnerID"))`. `OpIn` also works in `Where` and `Any`.
- **`NULL`.** `db.IsNull("OwnerID")` and `db.IsNotNull("OwnerID")` (`OpIsNull`, `OpIsNotNull`, no value) work on any field. A foreign key left at zero is stored as `NULL`, so `OwnerID = 0` never matches a row: use `IsNull` for unset keys.
- **`NOT`.** `db.Not` renders a literal `NOT (...)`, and a comparison with `NULL` is never true, so `Not(Cond("OwnerID", OpEq, 7))` leaves rows with an unset owner out. To include them: `db.Or(db.Not(db.Cond("OwnerID", db.OpEq, 7)), db.IsNull("OwnerID"))`. `Not(In(field, emptyList))` matches every row.
- **Shape.** A filter holds at most `db.MaxFilterNodes` (100) nodes and nests at most `db.MaxFilterDepth` (8) levels. An empty `And()` or `Or()`, a `Not` without exactly one child, and a malformed node are errors; no filter at all is a nil `Filter`.
- **Errors.** Every malformed query matches `errors.Is(err, db.ErrInvalidQuery)` (`errors.As` with `*db.QueryError` for the model and field); a query over a limit also matches `db.ErrQueryLimit` (`*db.QueryLimitError`). Nothing reaches the database first. `Query`, `Filter` and the lists in it are never modified.
- **`LIKE` and case.** `OpLike` is case-insensitive on SQLite and MySQL with default collations and case-sensitive on PostgreSQL.

This API does not support automatic wildcard escaping, joins, cross-model filtering, or filtering `Store.Get`. Related rows come only from `Query.Preload`, `Store.Related` and `Store.RelatedMany`, never from a filter on another model. Use raw SQL below for queries outside this scope.

## NULL and zero values

tanGO fields are plain Go values, never pointers or `sql.Null*` wrappers, so `NULL` and a field's zero value mean the same thing. Every read (`Get`, `List`, `Query`, `QueryRow`) turns a `NULL` column into the field's zero value: `""`, `0`, `false`, the zero `time.Time`, or the zero `model.Date`. That's what happens to existing rows when a migration adds a column: they read as the zero value until you write something else. A raw query's `LEFT JOIN` with no match reads the same way.

The write rule is the same on SQLite, PostgreSQL and MySQL. A foreign key field left at `0` is written as `NULL` (see [relationships](relationships-and-admin-foreign-keys.md)). A zero `time.Time` and a zero `model.Date` are written as `NULL` too. Nonzero values, and zero strings, numbers and booleans, keep their normal storage: `""` is stored as an empty string, and `0` as `0`.

Before this rule, a zero `time.Time` was written as year 0001 on SQLite and PostgreSQL and as `NULL` on MySQL. Only writes made after the change are `NULL`. A year-0001 value written earlier still reads as the zero `time.Time`, and nothing rewrites it. The migrations guide describes an optional cleanup.

`model.Date` is a calendar date with no time of day and no zone. It is stored as `DATE`, and its zero value means no date. Several zero dates are several `NULL` values, and they do not conflict under a `unique` constraint.

A `Where` condition compares with `=`, and `NULL` never equals anything. A zero time or zero date is not a value to compare with, so it is rejected rather than matched against `NULL`. Use `db.OpIsNull` (or `db.IsNull` in a `Filter`) to find `NULL` rows, and `db.OpIsNotNull` (or `db.IsNotNull`) to find set ones. A raw-SQL destination that needs to tell `NULL` from zero can use a pointer or `sql.Null*` field; those keep their usual meaning.

## Raw SQL

When the CRUD surface isn't enough, `Store` exposes two raw-SQL methods that reuse its scan-into-`dest` machinery:

- **`QueryRow(ctx, dest any, sql string, args ...any) error`** — one row into `dest`.
- **`Query(ctx, dest *[]T, sql string, args ...any) error`** — every row appended into `dest`, matching columns to struct fields by name (case-insensitively).

`Query` and `QueryRow` bind their arguments with the same rule as the ORM. A zero `time.Time` or zero `model.Date` passed as an argument returns `ErrInvalidQuery` before any SQL runs, on every dialect. Pass `nil` to bind NULL, or use `IsNull` through the ORM to find unset rows. Other arguments are bound as they are. Writes that set a time or date to unset should go through `Create` or `Update`, which store a zero time or zero date as NULL. A raw `INSERT` that takes a zero time must pass `nil` explicitly.

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

`sql` must already use the placeholder syntax matching the `Store`'s configured `Dialect` (`?` for SQLite and MySQL, `$1`/`$2`/... for Postgres) — neither method translates or validates placeholder syntax between dialects. See the [SQLite, PostgreSQL and MySQL setup guide](sqlite-and-postgresql-setup.md) for where that dialect is selected.

## Transactions with `InTx`

`Store.InTx(ctx, fn)` runs `fn` with a `*db.Store` bound to one transaction. Every method on it (`Create`, `Update`, `Get`, `List`, `Delete`, `Query`, `Exec`) uses that transaction and applies the same validation, including [bounded-string](models-and-tags.md#bounded-strings) and foreign-key checks.

```go
err := store.InTx(ctx, func(tx *db.Store) error {
	if err := tx.Create(ctx, accountMeta, &account); err != nil {
		return err
	}
	return tx.Create(ctx, profileMeta, &Profile{AccountID: account.ID})
})
```

It commits when `fn` returns nil and rolls back when `fn` returns an error, which `InTx` returns unchanged. A panic rolls back and is re-raised with the same value, so a programmer error is never turned into a returned error. Use `tx`, not the outer store, inside `fn`: the outer store is a different connection and does not see the transaction's writes.

Nesting is flat. `InTx` on a store that is already in a transaction joins it, with no savepoints, so an inner error rolls the work back only if it propagates out of the outermost `fn`. An operation that opens its own transaction when called directly, such as a PostgreSQL insert with an explicit ID or a cascading `Delete`, runs on the enclosing one. A `List` or `First` with a dotted `Query.Preload` path, and the `Related` and `RelatedMany` reads, do the same: inside `InTx` they read through the caller's transaction. See [loading related rows](relationships-and-admin-foreign-keys.md#nesting).

## Errors

`db.ErrNotFound` is the sentinel for "no matching row" on `Get`/`Update`/`Delete`; check it with `errors.Is`.

`db.IsUniqueConstraintViolation(err)` reports whether an error from `Create` or `Update` broke a unique constraint (a `tango:"unique"` field), on either dialect. Use it to let the database decide a duplicate: attempt the write and turn that error into your own answer (a `409`, a form error). A SELECT before the INSERT only narrows the race between two concurrent writers and never closes it, so keep it, if at all, as a friendlier path for the common case.

`db.ErrValueTooLong` is the sentinel for a [bounded string](models-and-tags.md#bounded-strings) longer than its `varchar=n` limit on `Create`/`Update`. Use `errors.As` with `*db.ValueTooLongError` for the model, field, limit (`Max`) and length (`Got`, in runes). The check runs before any SQL, on SQLite and PostgreSQL alike. `Store.Query`, `QueryRow` and any hand-written SQL skip it; PostgreSQL still rejects an over-long value there, SQLite stores it.

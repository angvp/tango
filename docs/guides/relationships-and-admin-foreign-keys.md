# Guide: relationships and admin foreign keys

A relationship in tanGO starts with a many-to-one **foreign key field**. On top of that field you can declare a relation field and load related rows explicitly, in two directions: `belongs_to` and `has_many`. No many-to-many, no reverse accessors, no automatic joins — see [limitations](../limitations.md) for the full boundary.

Together with the validation, explicit loading and cascade-delete behavior below, this is what tanGO calls its **minimal ORM foundations**: a small, deliberately bounded set of relationship-aware behavior across `model`, `db`, and `admin`. It is not a full ORM — there's no `QuerySet`, no lazy loading, no many-to-many, no reverse managers, no signals, no nested writes. See [where `Store` stops](#where-store-stops-and-raw-sql-begins) below for the boundary this implies.

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

## Validation layers

tanGO checks foreign keys and relations at different times, for different things:

- **Schema validation** — `Registry.Models().ValidateForeignKeys()` (called automatically by `Config.Check`, and so by `-check`) confirms that the *related model itself* is registered. It runs once, after every installed app finishes registering. Failure returns `model.ErrUnknownForeignKeyTarget`.
- **Relation validation** — `Registry.ValidateRelations()`, run by `Config.Check` beside `ValidateForeignKeys`, checks every relation field declared with `belongs_to=` or `has_many=`. Failure matches `model.ErrInvalidRelation`. See [loading related rows](#loading-related-rows).
- **Referential-integrity validation** — `Store.Create` and `Store.Update` confirm that a set foreign key field's value actually references an *existing row* of the related model, via a preflight `SELECT` before the write. Failure returns `db.ErrInvalidForeignKey`. A foreign key field left at its Go zero value (`0`) is treated as unset: it skips the check and is stored as `NULL`, so the database's `REFERENCES` constraint accepts it, and a `NULL` foreign key reads back as `0`. tanGO has no nullable-field mechanism, so this is a convention that relies on primary keys starting at `1` in practice, not a general "optional foreign key" feature.

Referential-integrity validation only runs when `Store.UseModels` has been called (the same gate that enables cascade delete, below), and is deliberately not wrapped in the same transaction as the write it guards — the generated DB-level `REFERENCES` constraint (see above) is what actually closes the narrow race this leaves open, when it's enabled.

## Naming collisions

Like every model name in tanGO, `fk=Author` refers to a bare, unnamespaced model name — if two reusable apps both register a model called `Author`, `fk=Author` is ambiguous in exactly the way described in [reusable apps](reusable-apps.md)'s naming-discipline section. Give models distinct names if you expect to install apps you don't control the naming of.

## Migrations and the generated constraint

`tango makemigrations` generates a real DB-level `REFERENCES` constraint for a foreign key field — `RESTRICT`/`NO ACTION`, never `CASCADE` at the database level (see below for why). On SQLite, this constraint is only enforced if `PRAGMA foreign_keys=ON` has been set on the connection. `db.ParseDSN` (and `tango.LoadDBConfigFromEnv`, which the generated scaffold uses) already returns a SQLite `Source` with the driver's `_foreign_keys=on` parameter, so an app configured through `TANGO_DB_DSN` gets enforcement with no extra step (see [configuration](configuration.md)). If you open your own SQLite connection from a bare path instead, add the parameter with `db.SQLiteForeignKeysDSN`:

```go
sqlDB, err := sql.Open("sqlite", db.SQLiteForeignKeysDSN(path))
```

Postgres and MySQL enforce the constraint natively with no extra step (on MySQL it is a table-level `FOREIGN KEY` clause, since InnoDB ignores a column-level `REFERENCES`).

A foreign key may point at a model in another app. You don't order anything for it: `tango migrate` creates the referenced app's table first and `tango migrate down` drops it last, regardless of app names or `InstalledApps` order — see [migrations](migrations.md#order-across-apps).

## Cascade delete

Deleting a row that other rows reference through a foreign key field **cascades**: `Store.Delete` finds every row across every registered model that points at the one being deleted, deletes them first (recursively — a chain of references is fully unwound), then deletes the target — all inside one transaction, with a cycle guard for circular or self-referential foreign keys. This is application-level cascade, not a database feature: the generated DB constraint stays plain `RESTRICT`, and only ever matters as a backstop against raw SQL or another tool deleting a referenced row directly, bypassing `Store.Delete`.

This is automatic for any app using the standard `Registry`/`Serve` flow — `Registry.SetStore` wires the model registry into the store for you. A `*db.Store` constructed and used entirely on its own (outside a `tango.Registry`) needs one explicit call to opt in:

```go
store := db.NewStore(sqlDB, dialect)
store.UseModels(modelRegistry) // enables cascade delete and referential-integrity validation; omit it and Delete only removes the target row, and Create/Update skip the row-level foreign key check
```

## Loading related rows

A relation field is a plain struct field that holds the related rows of a model. Declare it with a direction tag that names the foreign key it uses, and load it explicitly: with `Query.Preload` on a `List` or `First`, or with the batch reads `Store.Related` and `Store.RelatedMany`. Nothing loads implicitly. The canonical declarations are in `examples/api-with-admin/apps/posts/models.go` (a `belongs_to` author) and `examples/board/apps/posts/models.go` (an account and a list of comments).

### Declaring a relation field

```go
type Post struct {
	ID       int64            `tango:"pk"`
	Title    string
	AuthorID int64            `tango:"fk=Author"`
	Author   *Author          `tango:"belongs_to=AuthorID" json:"author,omitempty"`
	Comments []Comment        `tango:"has_many=PostID"`
}
```

- **`belongs_to=AuthorID`** goes on a pointer to a named struct. `AuthorID` is a foreign key of the owner, declared `fk=Author`. The relation is one related row per owner.
- **`has_many=PostID`** goes on a slice of named structs. `PostID` is a foreign key of the child, declared `fk=Post`, where `Post` is the struct that holds the field. The relation is every child whose key matches.
- A relation tag must be the field's only `tango` option.
- The foreign key's Go type must equal the primary key type of the model it refers to. A named type, such as `type AuthorID int64`, does not match `int64`, even with the same underlying type. `Registry.ValidateRelations` checks this and the names above at startup, and `Related` and `RelatedMany` check it again at call time.
- The relation field's struct must be the registered model's Go type, not only a type with the same name. A same-named struct from another package is refused by `Registry.ValidateRelations` with `ErrInvalidRelation`, which names the model and the field.

A relation field is not a column. It never appears in `ModelMeta.Fields`, so it generates no migration and the shell and migrations do not see it. `Store.Create` and `Store.Update` ignore it, so there are no nested writes: set the foreign key, not the relation. The host controls the JSON form with `json` tags. OpenAPI describes a `belongs_to` pointer as a nullable object and a `has_many` slice as an array, because it walks the struct's exported fields.

### `Query.Preload`

`Query.Preload` names the relation fields to load onto each returned row. `List` and `First` honour it; `Count` and `Exists` ignore it. A `Store` that has not called `UseModels` refuses a request with a `*QueryError` before any SQL runs, as does an unknown or repeated name.

```go
var posts []Post
err := store.List(ctx, postMeta, db.Query{
	Where:   []db.Condition{{Field: "AuthorID", Op: db.OpEq, Value: authorID}},
	OrderBy: []string{"-CreatedAt"},
	Preload: []string{"Author"},
}, &posts)
```

The parent query's `Where`, `Any`, `Filter`, `OrderBy`, `Limit` and `Offset` apply to the parent rows only. The relations load for the rows that come back. `First` takes the same `Query`, and a single row is read with `First`, not `List` with `Limit: 1`.

### `Store.Related` and `Store.RelatedMany`

The two batch reads underneath `Preload` are public. They take any slice of structs (or a non-nil pointer to one) and work on any `Store`, with or without `UseModels`. The caller names the key field on its rows. Only the related model's metadata is needed.

```go
authors := map[int64]Author{}
err := store.Related(ctx, posts, "AuthorID", authorMeta, authors)

comments := map[int64][]Comment{}
err = store.RelatedMany(ctx, posts, "ID", commentMeta, "PostID", db.Query{}, comments)
```

- `Related` fills a `map` from the related primary key to one related row. The key field must be an exported field directly on the row type whose type equals the related primary key type.
- `RelatedMany` fills a `map` from the key to a slice of children. Its `query` supplies `Where`, `Any`, `Filter` and `OrderBy` for the children. `Limit` and `Offset` are refused with a `*QueryError`, because they would cut across parents, and `Preload` is refused too, because it belongs to `List` and `First`. The refusals come before any statement is sent. The caller's `query` is never modified.
- Both write the destination only after every statement has succeeded. On any error the destination is left as it was passed.
- Both read through the transaction when called on a `Store` bound to one, such as the callback of `InTx`.
- Neither checks that the key field is declared `fk=`. The type check is the only check on the key.

### Missing data

The loading is lenient. None of these is an error:

- A zero foreign key sends no statement for that row, and its `belongs_to` pointer is `nil`.
- A foreign key with no matching row leaves the pointer `nil`. A key with no row adds no entry to a `Related` map.
- A `has_many` relation with no children is an empty, non-nil slice. A `nil` slice means the relation was not requested.
- Rows that share a foreign key value share one query key. Each row gets its own copy of the related struct, so changing one row does not change another.
- Children come in primary-key order, ascending, unless `OrderBy` is set.

### Query count

One read costs one statement for the parent rows, plus, for each relation, `ceil(distinct keys / 1000)` statements, where 1,000 is `db.MaxInValues`. The relation count follows the distinct keys, never one statement per row. A list with no rows sends no relation statement. A nested path adds one such count per segment, so the total depends on the distinct keys and the path segments of that read.

### Nesting

A relation can itself be preloaded. `Query.Preload` takes dotted paths such as `"Comments.Author"`: each segment is one relation read, one level deeper than the one before.

- A name implies its prefixes, so `"Comments.Author"` also loads `Comments`. Naming both `"Comments"` and `"Comments.Author"` is allowed; the second implies the first.
- A path may have at most `db.MaxPreloadDepth` (3) segments, and a `Query` may request at most `db.MaxPreloadPaths` (20) distinct relation reads. Beyond either, the error is a `*QueryLimitError`, which matches `db.ErrQueryLimit` and `db.ErrInvalidQuery`. Both are raised before any SQL runs.
- A path may revisit a model, which is how a self-referential relation is loaded. Each level is still bounded by the depth cap.

**Transactions.** A request with only one-level names opens no transaction; its reads are plain statements. A request with any dotted path runs the parent read and every relation read in one read-only `REPEATABLE READ` transaction, so every level sees the same committed data. Inside `InTx`, the same reads join the caller's transaction instead. The nested read holds one pooled connection for its duration.

**Consistency by dialect.** No later level of a nested read sees a write made during the read. PostgreSQL and MySQL give this through the snapshot. SQLite does not apply the isolation level (its driver issues a plain `BEGIN`), so it gives the guarantee by blocking: the read holds a shared lock, and a write from another connection is refused until the read ends. On a file database that write fails with a busy error once the driver's busy timeout ends. On an in-memory database the write waits for the single pooled connection, which the read holds, and fails when that wait ends. **On SQLite, a nested preload blocks writers for its duration.** Keep such reads short, or run them on a database that takes a snapshot.

### Not shipped

Joins, aggregates through relations, many-to-many, has-one, polymorphic relations, nested or cascading writes, inverse mutation APIs, lazy loading, a per-parent limit on children, and filtering parents by a related row. Each of these is raw SQL through `Store.Query` or `Store.QueryRow`, or is left out.

## Admin: the FK select and related labels

An `fk=`-tagged field renders as a `<select>` in the admin create/edit form, populated from every row of the related model, instead of a plain numeric input. What each option shows is controlled by `admin.Options.Label` on the *related* model's own registration — not the model with the foreign key:

```go
registry.Admin().Register(Author{}, admin.Options{
	Label: "Name", // shown wherever an Author is referenced elsewhere
})
```

The same `Label` value is used for the related-object column on any list page that includes the FK field in `ListDisplay`. If the related model has no `Label` configured — or isn't admin-registered at all — every FK display falls back to the raw primary key value; this is never a hard error, so adding a foreign key never breaks an existing admin registration that hasn't gotten around to setting `Label` yet.

The changelist does not query once per row. For each foreign-key column that has a label, it loads the labels of the whole page with one related read, so a page costs one row query plus one statement per labelled column. The create and edit forms load each foreign-key select with one read of the related model. Each foreign-key filter on a changelist reads its choices in one list read, plus one read for the active choice. The header of a single row, on its edit and delete pages, names the row by its first list column only, so it makes at most one label lookup, and only when that column is a foreign key. A label lookup never fails a page: if the related read fails, the column shows the raw keys and the error is logged. A column whose target has no `Label`, and one that a `Display` function owns, are not looked up. See [limitations](../limitations.md) for what the admin still does not let you change.

## Where `Store` stops and raw SQL begins

`Store` handles single-model CRUD, one-hop foreign key integrity (schema validation, referential-integrity validation and cascade delete, all described above), and explicit relation loading with `Query.Preload`, `Related` and `RelatedMany`. That's the whole of tanGO's minimal ORM foundations, by design — not a gap waiting to be filled. Anything that needs a join, an aggregate, or filtering across models is raw SQL via `Store.Query`/`QueryRow`, same as it is for every other kind of query tanGO doesn't have a typed helper for.

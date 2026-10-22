# Agent Recipe: Models

Use this when adding or changing a tanGO model.

Canonical examples: `examples/api-with-admin/apps/posts/models.go` and `examples/api-with-admin/apps/authors/models.go`.

## Do

- Define models as plain Go structs.
- Use exactly one primary key field tagged `tango:"pk"`.
- Use tags only for model metadata tanGO understands: `pk`, `unique`, `index`, `fk=ModelName`, and on a `string` field `varchar=n` (at most `n` characters, counted as runes) `text` (unbounded, the same as a bare `string`) or `json` (JSON text, validated when the model is written). Reach for `varchar=n` only when the field has a real maximum; a bare `string` stays unbounded.
- Register each model explicitly from the app's `Register(*tango.Registry)` path with `registry.Models().Register(Model{})`.
- Keep app-specific methods, validation, and services in ordinary Go code around the model.

Tiny shape:

```go
type Book struct {
    ID    int64  `tango:"pk"`
    Title string
}
```

## Don't

- Do not expect SQLite or raw SQL to enforce `varchar=n`; `Store.Create` and `Store.Update` do, and return `db.ErrValueTooLong`.
- A `tango:"json"` field is plain text that `Store.Create` and `Store.Update` check with `db.ErrInvalidJSON`; reads are not checked, `""` is unset, and adding the tag to an existing field generates no migration. Do not combine `json` with `varchar`, `text`, `pk`, `fk`, `unique` or `index`, and do not expect to query inside it. The [models guide](../guides/models-and-tags.md#json-fields) has the model and the audit loop for existing rows; both run as tests in `json_field_guide_test.go`.

- Do not create YAML/JSON schemas for models.
- Do not rely on filesystem scanning.
- Do not register from `init`.
- Do not import `internal/` helpers.

## Supported Field Direction

Stick to simple scalar Go fields and `time.Time` unless the public model guide says otherwise. For relationships, use `docs/agents/relationships.md`.

## Store Filtering

- Use `db.Query{Where: []db.Condition{...}}` with `Store.List` for AND conditions on the model's own fields. `Any` adds one OR group; the groups combine with AND. See `examples/api-with-admin/apps/posts/views.go` for a working `Where` (`author_id`) + `Any`/`OpLike` (`q`, across `Title`/`Body`) composition.
- Use `db.OpEq`/`OpNe` on any supported field; `OpGt`/`OpGte`/`OpLt`/`OpLte` only on numeric or `time.Time` fields. Match `Value` to the field's Go kind. `nil` is invalid; `false`, `0`, and `""` are literal values. Repeating a field permits a range.
- `db.OpLike` accepts SQL wildcard patterns on string fields. `Store.Count(ctx, meta, query)` counts matching rows without pagination. See `admin/list.go` for the `Any` + `OpLike` + `Count` composition, and `docs/guides/persistence-crud-and-raw-sql.md` for dialect differences.
- Use `Query.Filter` (`db.Cond`, `db.And`, `db.Or`, `db.Not`) for nested groups, `db.In(field, slice)` for a list of values (nil or empty matches nothing; at most 1,000 values), and `db.IsNull`/`db.IsNotNull` for unset foreign keys. `Store.First(ctx, meta, query, &row)` reads one row (primary-key order by default, `db.ErrNotFound` when none); `Store.Exists` answers yes or no.
- For automatic wildcard escaping, joins, cross-model filtering, or non-PK `Get` semantics, use `Store.Query`/`QueryRow` with explicit SQL.
- Don't filter an unset foreign key with `OwnerID = 0` or `OwnerID IN (0, 3)`: it is stored as `NULL`, so unset rows never match; use `db.IsNull("OwnerID")`, or `db.Or(db.In("OwnerID", ids), db.IsNull("OwnerID"))` for assigned IDs plus unset rows. Don't read one row with `List` and `Limit: 1` when `First` says it; `First` orders by primary key, so for an unordered lookup name a unique column.

## Check

- Compare with `examples/api-with-admin/apps/posts/models.go`.
- Then continue with `docs/agents/migrations.md` so the table exists.
- Run the shared checklist: `docs/agents/checklist.md`.

# Agent Recipe: Models

Use this when adding or changing a tanGO model.

Canonical examples: `examples/api-with-admin/apps/posts/models.go` and `examples/api-with-admin/apps/authors/models.go`.

## Do

- Define models as plain Go structs.
- Use exactly one primary key field tagged `tango:"pk"`.
- Use tags only for model metadata tanGO understands: `pk`, `unique`, `index`, and `fk=ModelName`.
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
- For nested groups, automatic wildcard escaping, `IS NULL`, joins, cross-model filtering, or non-PK `Get` semantics, use `Store.Query`/`QueryRow` with explicit SQL.

## Check

- Compare with `examples/api-with-admin/apps/posts/models.go`.
- Then continue with `docs/agents/migrations.md` so the table exists.
- Run the shared checklist: `docs/agents/checklist.md`.

# Agent Recipe: Relationships and Foreign Keys

Use this when one model should reference another model.

Canonical example: `examples/api-with-admin/apps/posts/models.go` references `examples/api-with-admin/apps/authors/models.go`. Human guide: `docs/guides/relationships-and-admin-foreign-keys.md`.

## Do

- Add an integer foreign-key field on the referencing model.
- Tag it with `tango:"fk=TargetModel"`, where `TargetModel` is the Go type name of the registered target model.
- Register both models before running checks/migrations.
- Regenerate migrations and confirm the FK reference appears as expected.
- Register both models with admin when the admin should render a select widget and quick-create affordance.
- Set `admin.Options{Label: "FieldName"}` on the related model when the FK select should show a friendly label.

Tiny shape:

```go
type Book struct {
    ID       int64 `tango:"pk"`
    AuthorID int64 `tango:"fk=Author"`
}
```

To read related rows in the same request, add a relation field next to the foreign key. It is not a column and needs no migration:

```go
type Post struct {
    ID       int64   `tango:"pk"`
    AuthorID int64   `tango:"fk=Author"`
    Author   *Author `tango:"belongs_to=AuthorID" json:"author,omitempty"`
}
```

- Use `belongs_to=FK` on a pointer to a named struct for the one related row, and `has_many=FK` on a slice of named structs for the children. `FK` is the foreign key field, on the owner for `belongs_to` and on the child for `has_many`.
- Load with `db.Query{Preload: []string{"Author"}}` on `Store.List` or `Store.First`. Use dotted names such as `"Comments.Author"` for nested levels, up to three segments and twenty distinct reads per query.
- Use `Store.Related` and `Store.RelatedMany` for batch reads into a caller-owned map, when the related rows are wanted apart from the parent rows.
- `Query.Preload` needs a `Store` that has called `UseModels`. `Related` and `RelatedMany` work on any store.

Canonical relation declarations: `examples/api-with-admin/apps/posts/models.go` (author) and `examples/board/apps/posts/models.go` (account and comments). Read path: `examples/api-with-admin/apps/posts/views.go` (`Preload: []string{"Author"}`).

## Don't

- Do not use an unregistered target model name.
- Do not assume FK fields automatically create custom joins or serializers.
- Do not bypass `db.Store` referential checks unless raw SQL is genuinely needed.
- Do not write a relation field. `Create` and `Update` ignore it; set the foreign key instead.
- Do not combine `belongs_to` or `has_many` with another `tango` option: `Register` refuses it with `model.ErrInvalidRelation`. Do not give a key a named type that differs from the primary key type, such as `type AuthorID int64` for an `int64` key: the startup check refuses it with `model.ErrInvalidRelation`.
- Do not expect lazy loading. A relation is loaded only when a query names it.
- Do not expect `Count` or `Exists` to load relations, and do not put `Limit` or `Offset` on `Store.RelatedMany`; it refuses them.
- Do not treat a nil `has_many` slice as "no children". A nil slice means the relation was not requested; a loaded relation with no children is an empty slice.
- Do not use a dotted `Preload` path on SQLite where other writers must not wait: the nested read holds a shared lock and blocks writers for its duration.

## Check

- Compare `Post.AuthorID` in `examples/api-with-admin/apps/posts/models.go`.
- Compare the related `Author` admin label in `examples/api-with-admin/apps/authors/app.go`.
- Compare the `Preload` read in `examples/api-with-admin/apps/posts/views.go` and the batch reads in `examples/board/apps/posts/queries.go`.
- Continue with `docs/agents/migrations.md` after changing relationships.
- Run the shared checklist: `docs/agents/checklist.md`.

# Agent Recipe: Relationships and Foreign Keys

Use this when one model should reference another model.

Canonical example: `examples/api-with-admin/apps/posts/models.go` references `examples/api-with-admin/apps/authors/models.go`.

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

## Don't

- Do not use an unregistered target model name.
- Do not assume FK fields automatically create custom joins or serializers.
- Do not bypass `db.Store` referential checks unless raw SQL is genuinely needed.

## Check

- Compare `Post.AuthorID` in `examples/api-with-admin/apps/posts/models.go`.
- Compare the related `Author` admin label in `examples/api-with-admin/apps/authors/app.go`.
- Continue with `docs/agents/migrations.md` after changing relationships.
- Run the shared checklist: `docs/agents/checklist.md`.

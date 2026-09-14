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

## Check

- Compare with `examples/api-with-admin/apps/posts/models.go`.
- Then continue with `docs/agents/migrations.md` so the table exists.
- Run the shared checklist: `docs/agents/checklist.md`.

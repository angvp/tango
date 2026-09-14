# Agent Recipe: Admin Registration

Use this when an existing model should appear in the tanGO admin.

Canonical example: `examples/api-with-admin/apps/posts/app.go`.

## Do

- Install `admin.New(store)` in the host project's `InstalledApps`.
- Register each model manually with `registry.Admin().Register(Model{}, admin.Options{...})`.
- Pick `ListDisplay` fields that identify the object well.
- Pick `Search` fields only for useful text lookups.
- Pick `Ordering` explicitly when list order matters.
- Use `Label` on related models when foreign-key selects should display a friendly value.

Tiny shape:

```go
registry.Admin().Register(Book{}, admin.Options{
    ListDisplay: []string{"Title"},
    Search: []string{"Title"},
})
```

## Don't

- Do not expect automatic admin registration from model registration.
- Do not expose or edit password hashes through generic admin forms unless a guide explicitly says it is safe.
- Do not reach into admin internals for custom behavior.

## Check

- Compare with `examples/api-with-admin/apps/posts/app.go` and `examples/api-with-admin/apps/authors/app.go`.
- Run the shared checklist: `docs/agents/checklist.md`.

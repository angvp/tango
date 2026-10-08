# Agent Recipe: Admin Registration

Use this when an existing model should appear in the tanGO admin.

Canonical example: `examples/api-with-admin/apps/posts/app.go`. Human guide: `docs/guides/admin-registration.md`.

## Do

- Install `admin.New(store)` in the host project's `InstalledApps`.
- Register each model manually with `registry.Admin().Register(Model{}, admin.Options{...})`.
- Pick `ListDisplay` fields that identify the object well.
- Pick `Search` fields only for useful text lookups.
- Pick `Ordering` explicitly when list order matters.
- Use `Label` on related models when foreign-key selects should display a friendly value.
- Use `Labels`, `HelpText`, `ReadOnly`, and `FieldOrder` only for clear form ergonomics; all keys are Go field names and are validated at registration.
- Use `Widgets` only when the built-in form widget is genuinely insufficient. Widgets are best-effort extensibility, not a stable long-term contract.
- Use `admin.WithBranding` or `admin.WithMiddleware` on `admin.New(store, ...)` for admin-wide branding or `/admin/`-scoped HTTP behavior.

Tiny shape:

```go
registry.Admin().Register(Book{}, admin.Options{
    ListDisplay: []string{"Title"},
    Search: []string{"Title"},
    Ordering: []string{"Title"},
    HelpText: map[string]string{"Title": "Public display title."},
})
```

## Don't

- Do not expect automatic admin registration from model registration.
- Do not expose or edit password hashes through generic admin forms unless a guide explicitly says it is safe.
- Do not reach into admin internals for custom behavior.
- Do not treat `ReadOnly` as an authorization system; it only controls the admin form.
- Do not set a foreign-key `Label` to a secret or unstable value just to make a select look nicer.

## Check

- Compare with `examples/api-with-admin/apps/posts/app.go` and `examples/api-with-admin/apps/authors/app.go`.
- For a foreign key, verify the related model is also admin-registered if editors should get the quick-create link.
- Run the shared checklist: `docs/agents/checklist.md`.

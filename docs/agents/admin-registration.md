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
- Use `Widgets` only when the built-in form widget is genuinely insufficient. Widgets are best-effort extensibility, not a stable long-term contract. A widget reports a problem with its own field by returning `admin.FieldError{Message: ...}` from `Parse` (several can be joined with `errors.Join`): admin re-renders the form with a 422 and shows each message beside its field. Any other `Parse` error is a single form-level message (see `docs/guides/admin-registration.md`).
- A `tango:"json"` field gets `admin.JSON()` by default; set `Widgets` only to replace it. Use `admin.JSON()` explicitly for a plain string field holding JSON and `admin.Markdown(...)` for authored text; pass `admin.WithMarkdownPreview(fn)` only with a renderer whose sanitization you have decided. Never assume admin sanitizes it.
- Use `Filters` for boolean and foreign-key fields operators filter by, and `Display` with `admin.DisplayFunc` for computed list cells; a `Display` column that is not a model field needs a `Labels` entry. Build a download link in your own authorized View and pass it as `Href`; admin must never learn storage keys.
- Set `BulkDelete: true` only on models where an operator deleting a page of rows at once is acceptable: it cascades like a single-row delete, needs JavaScript, never spans a page, and is all-or-nothing. Do not build a `delete-selected` form yourself; the route and field names are not a contract.
- Behind a reverse proxy, pass `admin.WithTrustedProxies(networks...)` (and `accounts.WithTrustedProxies` if `accounts` is installed) or every user shares one login-limiter bucket. Never trust `X-Forwarded-For` without it.
- Use the admin theme tokens (`bg-surface`, `text-ink`, `border-line`, the status family) in widget HTML, not raw palette colours, so a widget follows light and dark; see the theme table in `docs/guides/admin-registration.md`. The admin has no theming hook.
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

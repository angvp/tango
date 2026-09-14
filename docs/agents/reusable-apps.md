# Agent Recipe: Reusable Apps

Use this when packaging functionality as an importable app another tanGO project can install.

Canonical examples: `examples/reusable-greetings/` and `examples/reusable-greetings-host/`.

## Do

- Make the app a normal importable Go package.
- Expose either `var App tango.App` or `func New(...) tango.App`.
- Register models, routes, admin options, checks, templates, and static assets inside the app's `Register(*tango.Registry)` path.
- Let the host install the app explicitly in `InstalledApps`.
- If the app needs host dependencies such as `*db.Store`, accept them through `New(...)`.
- If the app ships migrations, expose them as a normal Go value and have the host combine them with host migrations in a deliberate order.

Tiny shape:

```go
func New(store *db.Store) tango.App { return App{store: store} }
```

## Don't

- Do not scan for reusable apps.
- Do not load plugins dynamically for v0.1 behavior.
- Do not copy reusable app internals into the host project.
- Do not import host project packages from the reusable app.

## Check

- Compare the app package with `examples/reusable-greetings/greetings/app.go`.
- Compare host installation with `examples/reusable-greetings-host/main.go`.
- For local project apps instead, use `docs/agents/project-shape.md`.
- Run the shared checklist: `docs/agents/checklist.md`.

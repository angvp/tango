# Widget interface owns both render and parse, not render-only

Admin needed a contract for customizing how one admin form field looks and behaves. The simplest option was render-only — a `Widget` only produces HTML, while parsing the submitted value stays on the existing generic, Go-kind-driven `setFieldFromString`. We rejected that in favor of giving `Widget` both responsibilities:

```go
type Widget interface {
    Render(FieldContext) template.HTML
    Parse(FieldContext, FieldValues, reflect.Value) error
}
```

A render-only contract can't express a widget that needs more than one HTML input for a single Go field — a date/time picker split into separate date and time controls, for instance — because the generic parser only ever reads one form value per field. Giving `Widget` its own `Parse`, receiving the whole submitted form (not just one raw string) via `FieldValues`, lets a widget assemble its field's value from however many inputs it rendered.

`Parse` also receives `FieldContext` — the same value passed to `Render` — not just `FieldValues` and the destination. Without it, a widget has no way to know which form key(s) belong to it: `FieldContext.Name` is the base name a simple widget reads directly (`values.Get(ctx.Name)`), and the stable prefix a multi-input widget derives its own field names from (e.g. `ctx.Name + "_date"`/`ctx.Name + "_time"`), used identically on both the render and parse side so the two stay in sync by construction rather than by convention the widget author has to remember twice.

This does mean a widget author takes on real reflection code instead of only HTML, and that `Parse` failures reuse today's generic, form-level error string rather than getting a richer per-field error contract — accepted rather than solving a problem (inline field validation UX) nothing has asked for yet.

Considered and rejected: a render-only contract (too narrow — can't outgrow one-input-per-field); a fuller field-component abstraction also owning list-display formatting (more surface than any concrete need justified; deferred until a real widget forces it).

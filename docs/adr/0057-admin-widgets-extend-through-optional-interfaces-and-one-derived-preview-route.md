# Admin widgets extend through optional interfaces, and admin owns one derived preview route

**Amendment:** `Render` and `Parse` now receive the request's `context.Context` as their first argument, so the sentence below that says `Parse` receives no request context, and the rejected alternative of passing a context to `Parse`, no longer hold. The change is in the Unreleased entry of the [CHANGELOG](../../CHANGELOG.md#unreleased); the preview route decision is unchanged.

A widget is the admin's seam for one field's control, and M50 needed two things a `Widget` could not provide without breaking every existing widget: a validation message that appears beside its own field, and a preview of a field's submitted text. Both are added as optional interfaces, and the `Widget` interface itself does not change.

A `Parse` may return `admin.FieldError{Message}` directly or inside an `errors.Join`. Admin extracts every such error, renders it beside the field it names with `aria-invalid="true"` and an `aria-describedby` reference, and keeps any other error as a form-level message as before. A preview is a `PreviewWidget`, a narrow interface with one method, `Preview(ctx, source)`. Admin type-asserts a field's widget for it. Only the Markdown widget built with a callback implements it, so a widget without one has no preview and no route.

Admin owns the route. For each model with at least one preview field it registers one POST route, `<model>/_preview/{field}/`. The route sits behind the same session check as every admin route, and only then does the handler look up the field: an unknown field, a primary key, a read-only field or a field whose widget is not a `PreviewWidget` is a 404. A widget has no way to register a route, and `Parse` still receives no request context.

Rejected:

- **Adding a method to `Widget` or a preview field to `FieldContext`.** Every custom widget in every host would stop compiling, and a preview would widen a type that every widget reads.
- **Letting a widget register its own routes.** It would move authentication, CSRF, size limits and the staff check out of admin's hands and into each widget.
- **Passing an `*http.Request` or a context to `Parse`.** It would widen every field's contract for one feature, and the one feature that needs the request already has its own route.
- **A `JSONField` or `MarkdownField` model type.** The semantics live in the admin's widgets, not in the model (D4).

Consequences: the built-in widgets and any host's widget keep working unchanged. `FieldError`, `PreviewWidget`, the route path and the JSON bodies are presentation details, not Covered API, as D10 records, so the route can move without a deprecation window. A host that wants a preview implements nothing beyond the callback. The route's limits (256 KiB of input, a five-second cooperative deadline, 1 MiB of output) belong to admin and are not configurable in M50.

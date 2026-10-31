# Upgrading

This page lists what a host application changes when it moves to the next release of tanGO. Each section names a change, what it affects and what to do about it. The full list of changes is in the unreleased section of the [changelog](../CHANGELOG.md).

## admin.Widget takes the request context

`Render` and `Parse` now receive the request's `context.Context` as their first argument, so a widget can translate its own text with `i18n.T(ctx, key, fallback)`. Change each method of a host widget like this:

```go
// before
func (w myWidget) Render(f admin.FieldContext) template.HTML
func (w myWidget) Parse(f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error

// after
func (w myWidget) Render(ctx context.Context, f admin.FieldContext) template.HTML
func (w myWidget) Parse(ctx context.Context, f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error
```

Use `_ context.Context` for the parameter when the widget has no text to translate, and import `context`. Direct calls to `Render` or `Parse` in host code and tests need the context argument as well. A type assertion to an interface with the old method shape still compiles and then reports false at run time, so search for those too.

`admin.Widget` is best-effort, so this is a change in a minor release rather than a compatibility break. `admin.PreviewWidget` and its `Preview` method are unchanged. The [host-widget migration section](compatibility.md#migrating-host-widgets) lists the other rules a host widget follows.

## Host CSS uses the token utilities

The admin no longer loads Tailwind from a CDN. Its stylesheet is precompiled and served from a content-hashed URL. Host widgets and templates should style with the admin's token utilities only: `text-`, `bg-`, `border-` and `ring-` with the admin's token names, including their opacity steps such as `text-ink/60`. Other Tailwind classes are not part of the admin's contract, because the stylesheet contains them only for the admin's own templates. A widget that needs other styling ships its own stylesheet from a content-hashed URL.

The [host-widget migration section](compatibility.md#migrating-host-widgets) gives the colour rules that apply to host widgets.

## storage.Serve sends a sandbox policy

Every GET and HEAD response for a stored object now carries `Content-Security-Policy: sandbox; default-src 'none'`. A browser that shows the object inline runs it in an opaque origin, with no script and no subresources. The most visible change is an inline PDF. To keep the previous response on one route, set `SkipSandboxPolicy: true` in the `storage.ServeOptions` that route passes to `Serve`. Error responses from `Serve` carry no policy.

See [delivering a file](guides/uploads.md#delivering-a-file) in the uploads guide.

## Admin error pages are HTML

Access denied, an expired form, a missing row, a throttled sign-in, a server failure and an unsupported method now answer with an HTML page in the admin's layout. The admin does not inspect the `Accept` header, so every caller receives the HTML page, including a caller that asks for JSON. The status codes do not change. A 405 response adds an `Allow` header that lists the methods the route handles.

Callers that read the JSON error body of these routes must read the HTML page instead. Two responses stay JSON: the Markdown preview route, and the 400 responses for an invalid density or theme value. When the app installs `tango.RequestID`, a server failure shows a short request reference on the page.

## The strict Content-Security-Policy is opt-in

The admin sends no `Content-Security-Policy` header by default. `tango.SecurityHeaders` is opt-in middleware, and installed with no options it applies `tango.StrictCSP()`. To run the admin under the policy its pages satisfy, install the middleware with the admin's policy:

```go
admin.New(store, admin.WithMiddleware(tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy()))))
```

Widget markup must then carry no inline `style` attribute, no inline event handler and no inline `<script>`. The [content security policy guide](guides/content-security-policy.md#the-admin) describes the policy, and the [host-widget migration section](compatibility.md#migrating-host-widgets) lists the widget rules.

## Renamed and removed admin message keys

Some `admin.*` message keys are renamed, replaced or removed. The [catalog impact](../CHANGELOG.md#catalog-impact) table in the changelog lists each key with what changed. Read the table before updating a host catalog.

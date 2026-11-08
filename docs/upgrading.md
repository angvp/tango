# Upgrading

This page lists what a host application changes when it upgrades from v0.7.0 to v0.8.0, and the last sections cover changes that are not yet released. Each section names a change, what it affects and what to do about it. The full list of changes is in the v0.8.0 section of the [changelog](../CHANGELOG.md).

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

## Unreleased: duplicate migration numbers are refused

A migration registry that contains two migrations with the same number in one app, or the same `(App, Name)` twice, is now refused. Before this change such a registry applied, with the order of the two migrations decided by name. Two branches that each added a migration with the same number and merged cleanly now fail until one of them is renumbered.

What to do:

1. Run `tango check`. It prints `check failed:` followed by `tango migration: conflicting migration names:` and one line per conflict, each naming the app and the migrations involved.
2. Rename one migration in each conflict to the next free number in its app, and set its `Name` to match the filename. Renumber only a migration that no database has applied: applied state is keyed on `(App, Name)`, so a renamed applied migration runs again.
3. Run `tango makemigrations` to regenerate `migrations/migrations.go`, then `tango check` until it passes, then `tango migrate`.

`tango makemigrations` and `tango tui` report the same conflicts, and `tango makemigrations` will not generate a new migration on top of a conflicting registry. There is no flag or environment variable to skip the check. See [migration numbers and conflicts](guides/migrations.md#migration-numbers-and-conflicts) for the full rule.

`-tango-dump-models` also refuses a conflicting registry, whichever other flag is given, and prints no models until the conflict is resolved.

`RollbackLast` (`tango migrate down`) is not subject to this check, so a registry with a conflict can still be rolled back. It now refuses an ambiguous tie or a target registered twice, before any `Down` runs, where it used to guess.

## A zero time is stored as NULL, and zero times are not query values

Two changes affect code that writes or queries a `time.Time` field. Both are unreleased.

**Writes.** A zero `time.Time` is now stored as `NULL` on SQLite and PostgreSQL, as it was on MySQL. Earlier versions wrote year 0001 on those two databases. Rows written before the change keep year 0001 and still read as the zero `time.Time`; nothing rewrites them. A unique constraint on a time column no longer collides on several unset values, and a NOT NULL time column added with a default receives `NULL` for a zero value.

**Queries.** A zero `time.Time` is no longer a query value. `=`, `!=`, the range operators and each `IN` element with a zero time return `ErrInvalidQuery` before any SQL runs. To find unset rows, use `db.IsNull` or `db.IsNotNull`:

```go
// before: matched year 0001 on SQLite and PostgreSQL, NULL on MySQL
db.Query{Where: []db.Condition{{Field: "EmailVerifiedAt", Op: db.OpEq, Value: time.Time{}}}}

// after
db.Query{Where: []db.Condition{{Field: "EmailVerifiedAt", Op: db.OpIsNull}}}
```

Ordering by an unset time sorts first in ascending order on SQLite and MySQL and last on PostgreSQL. Use the database's own `NULL` ordering in an application that relied on the earlier year-0001 order.

**Optional cleanup.** The migrations guide has SQL that converts historic year-0001 rows to `NULL` on SQLite and PostgreSQL. It is optional. tanGO does not generate or run it, and historic rows read as the zero time whether or not you run it.

The new `model.Date` type is described in the [models guide](guides/models-and-tags.md#supported-field-kinds).

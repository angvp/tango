# Guide: admin registration

tanGO's admin is a server-rendered HTML app: list, create, edit, and delete pages for any model you register with it, behind a real session-cookie login.

## Registering a model

Admin registration is separate from model registration, and typically lives right next to it in the same app:

```go
if err := registry.Models().Register(Post{}); err != nil {
	return err
}
if err := registry.Admin().Register(Post{}, admin.Options{
	ListDisplay: []string{"Title", "CreatedAt"},
	Search:      []string{"Title"},
	Ordering:    []string{"CreatedAt"},
}); err != nil {
	return err
}
```

`admin.Options` fields all reference **Go field names**, not column names, and are validated against the model's actual fields at registration time — a typo fails `Register` immediately with a clear error, never a silent no-op:

- **`ListDisplay`** — columns shown on the list page.
- **`Search`** — string fields a free-text search box filters against. Registration rejects non-string fields; search uses SQL `LIKE`, so `%` and `_` in the query retain wildcard meaning and case matching follows the database dialect.
- **`Ordering`** — the static default order the list page's rows are shown in (not interactive/click-to-sort). Prefix a field with `-` for descending, e.g. `"-CreatedAt"`.

## Adding the admin app

The admin is its own `tango.App`, constructed with just a `*db.Store` — there's no static credential to pass in, since accounts live in the database:

```go
admin.New(store)
```

Add it to `Config.InstalledApps` **after** every app that registers models with `registry.Admin()` — the admin app reads what's already been registered with `registry.Admin()` when its own `Register` runs, so order matters (per [`InstalledApps`'s ordering guarantee](configuration.md)).

Registering `admin.New` also registers two ordinary models of its own, `AdminUser` and `AdminSession` — they go through `tango makemigrations` like any other model, so running it after adding admin generates the migration that creates their tables. Neither is registered with the admin's own CRUD registry, so neither ever appears in the admin UI itself.

## Creating an admin account

There's no signup form and no scaffold-generated password: an admin account is created with the `tango admin` CLI, which hashes the password with bcrypt before it ever touches the database — no other code path writes a working password:

```sh
tango admin create <username>
```

It prompts for the password on stdin, so it never appears in shell history or a process listing. The account it creates has both `IsStaff` and `IsSuperuser` set to true — see [Staff and superuser access](#staff-and-superuser-access) below. Two more commands manage an account afterward, both invalidating its existing sessions:

- `tango admin resetpassword <username>` — rotate the password.
- `tango admin deactivate <username>` — disable the account without deleting its row (keeping audit history).

Four more commands change an existing account's `IsStaff`/`IsSuperuser` flags, without touching its password or `Active` state:

- `tango admin grant-staff <username>` / `tango admin revoke-staff <username>`
- `tango admin grant-superuser <username>` / `tango admin revoke-superuser <username>`

### How this actually runs

`tango admin create/resetpassword/deactivate/grant-staff/revoke-staff/grant-superuser/revoke-superuser` are the `tango` CLI's own convenience commands — under the hood, each just runs your project's own binary with an app-side flag:

- `-tango-admin-create=<username>` (optionally followed by `-tango-admin-no-staff` and/or `-tango-admin-no-superuser`)
- `-tango-admin-resetpassword=<username>`
- `-tango-admin-deactivate=<username>`
- `-tango-admin-grant-staff=<username>` / `-tango-admin-revoke-staff=<username>`
- `-tango-admin-grant-superuser=<username>` / `-tango-admin-revoke-superuser=<username>`

`create` and `resetpassword` prompt for the new password on stdin exactly as above; the rest need no password. These flags are handled by `admin.HandleCLI(ctx, store, os.Args[1:], stdin, stdout, stderr)`, which a generated `main.go` calls **before** `flag.Parse()`/`tango.DispatchFlags` — `tango newproject`'s scaffold wires this up for you, so you'd only add it yourself in a hand-rolled `main.go` that isn't built from the scaffold:

```go
if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
	return err
}
```

`HandleCLI` deliberately doesn't use the `flag` package: it and `tango.DispatchFlags` both run against the same `os.Args[1:]`, and `flag.Parse()` errors out on any flag it doesn't itself define — it can't tolerate seeing the other dispatcher's flags. A manual scan lets each dispatcher recognize only its own flags and ignore the rest, which is also why `HandleCLI` must run first: it's checked before anything calls `flag.Parse()` at all. You can invoke the flag directly instead of going through `tango admin create` — useful if you're running your binary directly rather than through the `tango` CLI:

```sh
echo "your-new-password" | ./yourapp -tango-admin-create=alice -tango-admin-no-staff -tango-admin-no-superuser
```

## Authentication

Admin routes require a valid session, established by logging in at `/admin/login/` — a real form, not a browser-native Basic Auth prompt. An unauthenticated request to any admin route redirects there with a `next` parameter, landing you back where you started after a successful login. `POST /admin/logout/` ends the session. Sessions have a fixed lifetime from creation (no idle timeout, no "remember me"); every form (including login) carries a CSRF token tied to the session, and repeated failed login attempts from the same source are rate-limited. See [limitations](../limitations.md) for the exact security boundary this implies.

## Staff and superuser access

Once a session is valid, one more check runs before a request reaches any admin route: `AdminUser.IsStaff`. An authenticated, active account with `IsStaff=false` gets `403 Forbidden` — distinct from the unauthenticated case above, which redirects to `/admin/login/` instead. A 403 rather than a redirect is deliberate: the account is genuinely logged in, and signing in again changes nothing, so a login redirect there would be actively misleading.

`IsStaff` is the only flag with real effect in v0.0.1. `AdminUser` also has `IsSuperuser`, but it is currently ignored: it has no distinct behavior, and it does not bypass `IsStaff` — a non-staff account with `IsSuperuser=true` still gets `403 Forbidden`, exactly like any other non-staff account. It exists purely as forward-compatible groundwork for a future, finer-grained permission bypass, so that a later milestone doesn't force every existing tanGO project through a second migration to add one boolean column.

Both flags default to `true`, for both new accounts and existing ones:

- `tango admin create <username>` with no flags produces `IsStaff=true, IsSuperuser=true` — the same "immediately usable full admin account" behavior `create` has always had. Pass `--no-staff` and/or `--no-superuser` to opt out at creation time (`-tango-admin-no-staff`/`-tango-admin-no-superuser` at the app-side flag layer).
- Upgrading an existing project to a tanGO version with these fields backfills every pre-existing `AdminUser` row to `IsStaff=true, IsSuperuser=true` — every account that could log in and use admin before the upgrade still can, unchanged.

There is no per-model, named, or object-level permission in v0.0.1 — `IsStaff`/`IsSuperuser` are the only tiers, and there's no `Group`/`Role` model or admin-UI-driven account management; every change to these flags goes through the CLI verbs above. App-level "require a named permission" is out of scope for tanGO entirely: an application wanting role or permission checks on its own routes wraps its own view the same way [`auth.RequireLogin`](application-auth.md#protecting-routes) does, against its own User model — tanGO owns no app-level User model to hang a generic primitive on.

## Routes

Once registered, a model at `/admin/<table>/` gets:

- `GET /admin/<table>/` — list, with pagination, sorting, and search.
- `GET`/`POST /admin/<table>/new/` — create.
- `GET`/`POST /admin/<table>/{pk}/` — edit.
- `GET`/`POST /admin/<table>/{pk}/delete/` — delete confirmation.

Every one of these requires an authenticated session; an unauthenticated request redirects to `/admin/login/` instead of the old `401`. `GET /admin/` (and `/admin`, without the trailing slash) redirects to the first registered model, sorted alphabetically by name — there's no separate "admin home" page to build. With nothing registered yet, it renders a minimal empty state instead of 404ing.

All of it runs against the same table your migrations created — there's no separate admin-specific schema (aside from `AdminUser`/`AdminSession` themselves, which are ordinary migrated tables too).

## Navigation and multiple models

Every model registered with the admin appears in a sidebar, sorted alphabetically, with the current model highlighted — you don't wire this up yourself; `admin.New` builds it once from `registry.Admin().Registrations()` and threads it through every page. Registering a second model is exactly the same call shown above, from a different app if you like:

```go
if err := registry.Models().Register(Author{}); err != nil {
	return err
}
if err := registry.Admin().Register(Author{}, admin.Options{
	ListDisplay: []string{"Name", "Email"},
}); err != nil {
	return err
}
```

Both `Post` and `Author` now show up in the sidebar, and `/admin/` redirects to `/admin/author/` (alphabetically first).

## Foreign key quick-create

For a field tagged as a foreign key, the admin renders a `<select>` populated from the related model. If that related model is also registered with the admin, tanGO shows a small `+` link next to the select. Clicking it opens the related model's create page, then returns to the original form after save with the newly-created object already selected.

For example, if `Book.AuthorID` is tagged `tango:"fk=Author"` and both `Book` and `Author` are admin-registered, the `Book` form gets a `+` beside the `Author` select. This is intentionally create-only: there is no edit link for the selected related object, no delete/remove action, no autocomplete, no many-to-many editing, and no inline formset support.

This is a full-page navigation, not a popup. Any unsaved changes in the parent form are lost when you click `+`; save the parent object first if you need to preserve those values. That trade-off is deliberate for v0.0.1 so the feature stays small and predictable.

## List page display

List and form field labels are humanized for display — `CreatedAt` renders as "Created At", `UserID` as "User ID" — without you naming anything twice; the underlying field name (used for sorting, search, and form submission) is untouched. The list page also reports a true total row count and a paginator with page-number links, using a Django-admin-inspired truncated-range presentation once there are many pages (`1 2 … 7 8 9 10 … 19 20`).

## Customizing form fields

Beyond `ListDisplay`/`Search`/`Ordering`/`Label`, `admin.Options` has four more field-name-keyed controls for the create/edit form, all validated against the model's real fields the same way:

- **`Labels`** — override a field's humanized default label.
- **`HelpText`** — descriptive text shown with a field.
- **`ReadOnly`** — fields that render non-editably. On edit, a read-only field keeps its existing stored value no matter what the submitted form contains for it; on create, it stays at its Go zero value. Submitted data for a read-only field is always ignored, never parsed — this is true regardless of any `Widgets` entry for that field, since read-only rendering bypasses the field's widget entirely.
- **`FieldOrder`** — render fields in this order; fields not listed keep their default order, appended after the ordered ones.

```go
registry.Admin().Register(Post{}, admin.Options{
	Labels:     map[string]string{"Title": "Post title"},
	HelpText:   map[string]string{"Published": "Unpublished posts stay hidden from public views."},
	ReadOnly:   []string{"CreatedAt"},
	FieldOrder: []string{"Title", "Body", "Published"},
})
```

## Custom field widgets

An `admin.Widget` replaces how one field renders and parses in the create/edit form — the escape hatch for anything the built-in text/number/date-time/checkbox/foreign-key-select behaviors don't cover, without forking any admin template:

```go
type Widget interface {
	Render(f FieldContext) template.HTML
	Parse(f FieldContext, form FieldValues, dest reflect.Value) error
}
```

`FieldContext` carries what a widget needs — field name, label, help text, current value, read-only flag, and (for a foreign key field) the related model's select options plus an optional related-object create URL — and is passed to *both* `Render` and `Parse`, so a widget can derive its own submitted form key(s) from `FieldContext.Name` identically on both sides. A simple widget reads `form.Get(f.Name)`; a widget needing more than one HTML input for its one Go field (a date/time picker split into separate controls, say) derives extra names from `f.Name` — e.g. `f.Name + "_date"` and `f.Name + "_time"` — using the same derivation in `Render` and `Parse` so the two never drift apart.

Set a widget per field via `Options.Widgets`:

```go
registry.Admin().Register(Post{}, admin.Options{
	Widgets: map[string]admin.Widget{
		"Body": admin.Textarea(), // the one built-in beyond the default five
	},
})
```

A widget's `Render` output may include its own `<link>`/`<script>` tags for CSS/JS it needs — typically served via its owning app's own `embed.FS` route (see [reusable apps](reusable-apps.md)), the same convention any app already uses for static assets. There's no shared deduplication mechanism, so that output must be **idempotent**: safe to emit once per rendered instance of the widget, since the same widget used on multiple fields will render its tags more than once. `examples/reusable-greetings`'s `Greeting.Name` field uses a real custom widget shipped by a reusable app this way — see its `greetings/widget.go`.

A `Parse` failure surfaces exactly like a built-in widget's parse failure would: a generic form-level error string, not a per-field inline error — that's a real admin-UX feature, but a separate one this doesn't provide yet.

## Branding

`admin.WithBranding` replaces the sidebar's generic title and mark with your own name and logo — the only two customization slots outside per-field widgets; there is no broader template or theme override mechanism:

```go
admin.New(store, admin.WithBranding(admin.Branding{
	Name:    "Acme Admin",
	LogoURL: "/static/logo.png",
}))
```

Omit it — `admin.New(store)` with no options — and the sidebar shows tanGO's generic default, unchanged.

## Stability

`Widget`, the `Options` fields this section and the one above describe, the built-in widgets, and `Branding` are **best-effort**, not one of tanGO's [stable v0.0.1 contracts](../limitations.md#stable-v001-cli-app-side-flags): admin's internals are still expected to evolve, and this surface may change without the advance-notice process those contracts get. This is different from `docs/limitations.md`'s "APIs still expected to change before a stable release" — that section names things expected to *settle* before v0.0.1 ships; this surface is expected to keep evolving even after.

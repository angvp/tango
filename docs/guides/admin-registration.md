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
- **`Display`** — computed changelist cells, keyed by a `ListDisplay` name. See [Computed changelist cells](#computed-changelist-cells-with-display).
- **`Ordering`** — the static default order the list page's rows are shown in (not interactive/click-to-sort). Prefix a field with `-` for descending, e.g. `"-CreatedAt"`.
- **`Filters`** — boolean and foreign key fields shown as filter selects above the list. A boolean offers Any, Yes and No. A foreign key offers Any plus the related rows, labelled as the related model's `Label` and ordered by primary key, as in the foreign key select. A foreign key whose related model has more than 100 rows offers no choices and says why, but still filters by a primary key given in the URL. Registration rejects any other kind of field. See [Changelist filters](#changelist-filters).
- **`SingularName` and `PluralName`** — the noun for one row and for several rows in messages and confirmations, such as "Delete this book". Unset, the humanized Go name of the model is used.

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

It prompts for the password on stdin without showing what you type, so it never appears in shell history or a process listing. For CI and containers, set `TANGO_ADMIN_PASSWORD` and the command uses it without prompting; an empty value counts as unset, and the variable wins over piped stdin. The command prints `using the password from TANGO_ADMIN_PASSWORD` (never the value) so a stale export does not go unnoticed. The environment is visible to process-inspection tools on the host, so prefer your platform's secret mechanism for anything long-lived. The scaffold loads `.env`, which may carry the variable: keep `.env` out of version control (the scaffold's `.gitignore` already lists it). The account it creates has both `IsStaff` and `IsSuperuser` set to true — see [Staff and superuser access](#staff-and-superuser-access) below. Two more commands manage an account afterward, both invalidating its existing sessions:

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

Admin routes require a valid session, established by logging in at `/admin/login/` — a real form, not a browser-native Basic Auth prompt. An unauthenticated request to any admin route redirects there with a `next` parameter, landing you back where you started after a successful login. `POST /admin/logout/` ends the session and, like every other admin POST, needs the session's CSRF token (a missing or wrong one gets 403 and the session stays); the sidebar footer shows the signed-in username and a **Log out** button that posts to it. Sessions have a fixed lifetime from creation (no idle timeout, no "remember me"); every form (including login) carries a CSRF token tied to the session, and repeated failed login attempts from the same source are rate-limited. See [limitations](../limitations.md) for the exact security boundary this implies.

## Staff and superuser access

Once a session is valid, one more check runs before a request reaches any admin route: `AdminUser.IsStaff`. An authenticated, active account with `IsStaff=false` gets `403 Forbidden` — distinct from the unauthenticated case above, which redirects to `/admin/login/` instead. A 403 rather than a redirect is deliberate: the account is genuinely logged in, and signing in again changes nothing, so a login redirect there would be actively misleading.

`IsStaff` is the only flag with real effect. `AdminUser` also has `IsSuperuser`, but it is currently ignored: it has no distinct behavior, and it does not bypass `IsStaff` — a non-staff account with `IsSuperuser=true` still gets `403 Forbidden`, exactly like any other non-staff account. It exists purely as forward-compatible groundwork for a future, finer-grained permission bypass, so that a later milestone doesn't force every existing tanGO project through a second migration to add one boolean column.

Both flags default to `true`, for both new accounts and existing ones:

- `tango admin create <username>` with no flags produces `IsStaff=true, IsSuperuser=true` — the same "immediately usable full admin account" behavior `create` has always had. Pass `--no-staff` and/or `--no-superuser` to opt out at creation time (`-tango-admin-no-staff`/`-tango-admin-no-superuser` at the app-side flag layer).
- Upgrading an existing project to a tanGO version with these fields backfills every pre-existing `AdminUser` row to `IsStaff=true, IsSuperuser=true` — every account that could log in and use admin before the upgrade still can, unchanged.

There is no per-model, named, or object-level permission — `IsStaff`/`IsSuperuser` are the only tiers, and there's no `Group`/`Role` model or admin-UI-driven account management; every change to these flags goes through the CLI verbs above. App-level "require a named permission" is out of scope for tanGO entirely: an application wanting role or permission checks on its own routes wraps its own view the same way [`auth.RequireLogin`](application-auth.md#protecting-routes) does, against its own User model — tanGO owns no app-level User model to hang a generic primitive on.

## Routes

Once registered, a model at `/admin/<table>/` gets:

- `GET /admin/<table>/` — list, with pagination, search, filters and the ordering you configure with `Options.Ordering` (column headers are not clickable).
- `GET`/`POST /admin/<table>/new/` — create.
- `GET`/`POST /admin/<table>/{pk}/` — edit.
- `GET`/`POST /admin/<table>/{pk}/delete/` — delete confirmation.

Every one of these requires an authenticated session; an unauthenticated request redirects to `/admin/login/` instead of the old `401`. `GET /admin/` (and `/admin`, without the trailing slash) redirects to the first registered model, sorted alphabetically by name, unless the app sets `admin.WithDashboard` (see the [admin dashboard guide](admin-dashboard.md)). With nothing registered yet, it renders a minimal empty state instead of 404ing.

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

For a field tagged as a foreign key, the admin renders a `<select>` populated from the related model. If that related model is also registered with the admin, tanGO shows an "Add new author" link, with a `+` before it, next to the select. Clicking it opens the related model's create page, then returns to the original form after save with the newly-created object already selected.

The select starts with a "No author selected" choice, because a foreign key at its zero value is unset (stored as NULL). The choice is the English text of `admin.fk.none_selected` ("No %s selected", filled with the related model's singular display name), so a host catalog can word it. A row whose foreign key is unset shows the choice selected on edit, and an em dash in the changelist rather than `0`. The built-in select has no required-foreign-key mode: tanGO has no required/optional distinction for foreign keys, so every one offers that choice.

For example, if `Book.AuthorID` is tagged `tango:"fk=Author"` and both `Book` and `Author` are admin-registered, the `Book` form gets an "Add new author" link beside the `Author` select. This is intentionally create-only: there is no edit link for the selected related object, no delete/remove action, no autocomplete, no many-to-many editing, and no inline formset support.

This is a full-page navigation, not a popup. Any unsaved changes in the parent form are lost when you click `+`; save the parent object first if you need to preserve those values. That trade-off is deliberate so the feature stays small and predictable.

## List page display

List and form field labels are humanized for display — `CreatedAt` renders as "Created At", `UserID` as "User ID" — without you naming anything twice; the underlying field name (used for sorting, search, and form submission) is untouched. The list page also reports a true total row count and a paginator with page-number links, using a truncated-range presentation once there are many pages (`1 2 … 7 8 9 10 … 19 20`).

### Computed changelist cells with `Display`

`Display` lets the host compute what a changelist cell shows from the whole row, for values a plain column cannot express: a file's size in human units, a shortened digest, or a link the host has already authorized. It is set per column name:

```go
admin.Options{
	ListDisplay: []string{"Name", "Size", "Download"},
	Display: map[string]admin.DisplayFunc{
		"Size":     func(_ context.Context, row any) admin.Display {
			doc := row.(*Document)
			return admin.Display{Text: admin.ByteSize(doc.Size)}
		},
		"Download": func(_ context.Context, row any) admin.Display {
			doc := row.(*Document)
			return admin.Display{Text: "Download", Href: "/documents/" + strconv.FormatInt(doc.ID, 10) + "/download/"}
		},
	},
	Labels: map[string]string{"Download": "File"},
}
```

- A `Display` key is a registered field (its value is replaced) or a **synthetic column**: a name in `ListDisplay` that is not a model field. A synthetic column must have a `Labels` entry for its header. Registration rejects a `Display` key that is not in `ListDisplay`, a nil function, a synthetic column without a `Labels` entry, and a synthetic name in any other option (`Search`, `Ordering`, form options).
- The function receives a pointer to the model row, so it can read any field. An empty `Display` yields an empty cell.
- `Text` is always HTML-escaped. `Href` is a link only when it is a relative URL (`/path`, `path`, `?q=1`, `#frag`) or an absolute `http`/`https` URL with a host. Scheme-relative `//host`, backslashes, whitespace or control characters, userinfo and every other scheme (`javascript:`, `data:`, ...) are refused, and the cell falls back to plain escaped text. A link is rendered with `rel="noopener noreferrer"`. If `Text` is empty the link shows its URL.
- A panic in the function is a bug in the host. Admin does not recover it: behind `tango.Recoverer()` (or your own recovery middleware) it becomes a 500, otherwise it propagates like any other panic.
- `admin.ByteSize(int64)` formats a byte count with binary units (`1.5 MiB`), and `admin.ShortHash(string)` keeps the first twelve characters of a digest.

Admin never reads storage, builds URLs or exposes keys on its own. For files, the host's `Display` supplies the link, and that link must point at a route the host authorizes; the uploads example links to its owner-only download View (see [`examples/uploads`](../../examples/uploads/README.md)).

## Changelist filters

`Filters` names boolean and foreign key fields. Each one becomes a labelled `<select>` beside the search box, submitted with the search as a plain GET form, so it works without JavaScript:

```go
if err := registry.Admin().Register(Post{}, admin.Options{
	ListDisplay: []string{"Title", "AuthorID", "Published"},
	Ordering:    []string{"-CreatedAt"},
	Filters:     []string{"AuthorID", "Published"},
}); err != nil {
	return err
}
```

- A boolean filter applies `yes` or `no`; `any` (or no parameter) applies nothing.
- A foreign key filter applies one related primary key. Its choices come from one query on the related model, in primary key order, capped at 100. The cap limits what is listed, not what can be filtered. Above it the filter shows "too many values to list" and offers no select, but a value in the URL (a bookmark, say) is looked up as that one related row and applied when it exists, with its label shown and kept when the form is resubmitted. A value that is malformed or names no row is ignored. The extra lookup happens only when a value is supplied.
- Filters combine with search and ordering using AND. The row count, the pagination and the pagination links all use the filtered set, and each link carries the search and the applied filters, encoded.
- Unknown field names, malformed values and foreign key IDs that the related model does not have are ignored. Only applied filters appear in the page state.

Each filter is a query parameter named `filter_` followed by the column name: `filter_published=yes`, `filter_author_id=3`. Treat these names as presentation details that may change between releases, not as a stable interface.

## Bulk delete

`BulkDelete: true` lets a staff account delete several rows of one model at once. It is off by default and set per model:

```go
if err := registry.Admin().Register(Comment{}, admin.Options{
	ListDisplay: []string{"PostID", "Author", "CreatedAt"},
	BulkDelete:  true,
}); err != nil {
	return err
}
```

The changelist gains a checkbox on each row, a select-all box and a **Delete selected** button. Pressing it opens a dialog that lists the checked rows (by the first `ListDisplay` value, or by the primary key when that is blank) and warns that rows referencing them are deleted too. Confirming sends one POST to `/admin/<model>/delete-selected/`; the changelist then shows how many rows were deleted and keeps the search, page and filters it had.

- **Who:** any staff account, with the same session and CSRF checks as every admin form. `IsSuperuser` has no effect.
- **One page at most:** the checked rows of the page in view, never more than one page. There is no "select everything that matches" and no selection across pages.
- **All or nothing:** the rows are deleted in one transaction. Inside it, every selected row is read again before anything is deleted; if any of them has no row then, the whole action is rejected with a 400, the transaction is rolled back, nothing is deleted and no event is logged. Rows that reference a deleted row are deleted with it, as in a single-row delete, and the dialog does not count them. Once deletion starts, a selected row that is already gone when its turn comes is accepted: another selected row's cascade removed it, or another transaction removed it after the re-read. Rows are removed in ascending key order and every selected row must be absent at the end, so the result does not depend on the order the keys were submitted in.
- **The server validates the selection again.** A request with no keys, more keys than a page, a `count` that differs from the keys sent, a repeated key, or a key that does not parse as the field's own type is rejected as a whole with a 400. Nothing is deleted and no event is logged. A numeric primary key must be canonical: the decimal digits of the value as the field would print it, with no sign other than `-` on a signed field, no leading zeros and no whitespace (`01`, `+5` and `-0` are refused), within the field's own width, and an unsigned key may not exceed the `int64` maximum. A string primary key is submitted and compared verbatim (an empty string is refused, and `"a "` does not match the row `"a"` whatever the database collation).
- **Needs JavaScript:** the controls are hidden, and the confirmation dialog is a script, so a browser without JavaScript sees the changelist as before.
- **The trail:** each committed bulk delete logs one `admin.EventBulkDelete` (`tango.admin.bulk_delete`) event with the model, `selected_count` and the account's username. `selected_count` is the number of rows the accepted action selected, not the number of database rows physically deleted: rows removed by a cascade are not added, and a selected row that was already gone when its turn came, such as one removed after the transaction's re-read, is still counted. It never logs keys or values. There is no audit table.

The `BulkDelete` option and the guarantees above are covered by the [compatibility promise](../compatibility.md#the-covered-api). The markup, the route path and the form field names are presentation details that may change between releases.

## Customizing form fields

Beyond `ListDisplay`/`Search`/`Ordering`/`Label`, `admin.Options` has four more field-name-keyed controls for the create/edit form, all validated against the model's real fields the same way:

- **`Labels`** — override a field's humanized default label, on forms and as its changelist column header. A field with no `Labels` entry keeps its default, so a registration only changes when it opts in. A synthetic `Display` column must have an entry.
- **`HelpText`** — descriptive text shown below a field on the create and edit forms. The built-in controls reference it with `aria-describedby`.
- **`ReadOnly`** — fields that render as a read-only text input (focusable and copyable, but never submitted). On edit, a read-only field keeps its existing stored value no matter what the submitted form contains for it; on create, it stays at its Go zero value. Submitted data for a read-only field is always ignored, never parsed — this is true regardless of any `Widgets` entry for that field, since read-only rendering bypasses the field's widget entirely.
- **Bounded strings** (`tango:"varchar=n"`) render `<input type="text" maxlength="n">`. Browsers count `maxlength` in UTF-16 units, so the server stays the authority: a create or edit whose value has more than `n` characters (runes) re-renders the form with `Use at most 5 characters. You entered 6.` beside the field, keeps everything else the person typed, and stores nothing. The message is the translatable key `admin.error.max_length` (arguments: the limit, the length; English fallback "Use at most %d characters. You entered %d."). Bare strings and `text` render as they always did, with no textarea; use `admin.Textarea()` for a multi-line field.
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
	Render(ctx context.Context, f FieldContext) template.HTML
	Parse(ctx context.Context, f FieldContext, form FieldValues, dest reflect.Value) error
}
```

`ctx` is the request's context. A widget that shows its own text translates it with `i18n.T(ctx, "your.key", "English text")`, so a host catalog can word it (see [i18n](i18n.md)).

`FieldContext` carries what a widget needs — field name, label, help text, the field's error message after a failed save (`Error`), current value, maximum length (`MaxLength`, `0` when unbounded), read-only flag, and (for a foreign key field) the related model's select options plus an optional related-object create URL — and is passed to *both* `Render` and `Parse`, so a widget can derive its own submitted form key(s) from `FieldContext.Name` identically on both sides. A simple widget reads `form.Get(f.Name)`; a widget needing more than one HTML input for its one Go field (a date/time picker split into separate controls, say) derives extra names from `f.Name` — e.g. `f.Name + "_date"` and `f.Name + "_time"` — using the same derivation in `Render` and `Parse` so the two never drift apart.

A widget reports a problem with its own field by returning `admin.FieldError{Message: ...}` from `Parse`, directly, wrapped with `%w`, or inside `errors.Join`. Admin collects every `FieldError` from every field, re-renders the form with a 422, shows each message beside its field and marks that field's built-in control `aria-invalid="true"` with `aria-describedby` pointing at the help text and error. Any other `Parse` error is shown beside its own field as a generic message, such as `Invalid value`, and the error's own text is never shown. A custom widget can read `FieldContext.Error` and `FieldContext.DescribedBy()` to do the same in its own markup.

Set a widget per field via `Options.Widgets`:

```go
registry.Admin().Register(Post{}, admin.Options{
	Widgets: map[string]admin.Widget{
		"Body": admin.Textarea(), // a multi-line text field; admin.JSON() is covered below
	},
})
```

Without an entry, a field gets the default widget for its Go type, with one exception: a field declared `tango:"json"` (see [models and tags](models-and-tags.md)) defaults to `admin.JSON()`. An explicit `Options.Widgets` entry for that field always wins, so `admin.Textarea()` there gives a plain textarea. A `ReadOnly` field stays the generic readonly input whatever its tag or widget, and the changelist is unchanged. Because a plain-text override skips the JSON widget's own check, a save refused by the store for invalid JSON still re-renders the form with `This is not valid JSON. Check that commas, quotes and brackets are in place.` (translation key `admin.error.invalid_json`) beside the field, keeps the text as typed and stores nothing.

A widget's `Render` output may include its own `<link>`/`<script>` tags for CSS/JS it needs — typically served via its owning app's own `embed.FS` route (see [reusable apps](reusable-apps.md)), the same convention any app already uses for static assets. There's no shared deduplication mechanism, so that output must be **idempotent**: safe to emit once per rendered instance of the widget, since the same widget used on multiple fields will render its tags more than once. `examples/reusable-greetings`'s `Greeting.Name` field uses a real custom widget shipped by a reusable app this way — see its `greetings/widget.go`.

A `Parse` error that is not an `admin.FieldError` is shown beside its own field as a generic message, such as `Invalid value`, and the error's own text is never shown. Return `admin.FieldError` to show your own message beside the field.

## JSON fields

`admin.JSON()` is a built-in widget for a string or text field that holds a JSON document. A field declared `tango:"json"` uses it by default, with no `Options.Widgets` entry needed. For any other string field, set it per field, like any widget:

```go
registry.Admin().Register(Profile{}, admin.Options{
	Widgets: map[string]admin.Widget{"Preferences": admin.JSON()},
})
```

An explicit entry for a `tango:"json"` field replaces the default, and the store still refuses invalid JSON on save. A `ReadOnly` field renders as the generic readonly input.

- **Validation on save.** The value must be one complete JSON document, checked by Go's decoder. A refused save keeps the exact text the person typed, re-renders it in the textarea, and shows a readable error beside the field, for example `This is not valid JSON at character 9. Check that commas, quotes and brackets are in place.` The position is in characters, counted from 1, and appears only when Go's decoder reports one. The decoder's own message is never shown. The textarea is `aria-invalid` and described by the error, and nothing is stored.
- **Stored exactly as submitted.** There is no canonicalization: spacing, key order and a trailing newline are saved as typed. The widget does not check structure or a schema.
- **Empty is not parsed.** An empty value is saved as the empty string, with the field's normal empty and required behaviour and no extra rule from the widget.
- **Formatted tab.** Browser-side, the field has two tabs, **Edit** and **Formatted**. **Formatted** shows `JSON.stringify(JSON.parse(text), null, 2)` of the current text; it is a view and never changes what you will save. Invalid input leaves that box empty, and the status line shows the sentence for invalid JSON instead. When the formatting differs from the text, **Use this formatting** copies it into the textarea and returns to **Edit**. The tabs appear only when the shared script runs; they follow the ARIA tabs pattern, with Left, Right, Home and End moving between them.
- **Without JavaScript** the field is an ordinary monospace textarea, and server validation is the only check.
- **Type.** Use it on a `string` or `text` field. On any other Go kind the save reports a form-level error naming the field.

### The shared widget script

`admin.JSON()` emits no inline script. Every form page includes one `<script src="/admin/_assets/widgets.<hash>.js" defer>` tag, and an `admin.JSON()` field adds its own copy of the same tag. The admin serves that file from its own embedded assets. The URL is content-hashed from the file's exact bytes and served with `Cache-Control: public, max-age=31536000, immutable`. The route, the file name and the script's contents are presentation details, not part of tanGO's compatibility promise. Repeated tags are harmless: the script guards itself, so a second copy does nothing. The source is `admin/assets/widgets.js`, which the admin's own built-in widgets use. It is not a public extension point: a third-party widget that needs behaviour should ship its own script, as described above. The admin sends no Content-Security-Policy of its own. A host that composes `admin.ContentSecurityPolicy()` into the admin tier, as the [content security policy guide](content-security-policy.md#the-admin) describes, runs the admin's pages under that policy, and its `script-src 'self'` allows this script.

## Markdown fields

`admin.Markdown()` is a built-in widget for an authored text field: a monospace, resizable textarea with a server-rendered character count (`n characters`, or `n / max characters` for a bounded field). It stores the submitted text exactly as typed, with no normalization.

```go
registry.Admin().Register(Profile{}, admin.Options{
	Widgets: map[string]admin.Widget{"Bio": admin.Markdown()},
})
```

**The host owns rendering and sanitization.** tanGO ships no Markdown parser, sanitizer or rich-text dependency, and the admin never renders the stored text. Any public page that shows the Markdown must render and sanitize it under the application's own policy; an operator's text is not safe to output as HTML just because it was entered in the admin. `examples/spa-accounts` edits a profile's `Bio` this way and renders nothing.

`admin.WithMarkdownPreview(fn)` gives the widget a host renderer of type `admin.MarkdownPreview`, `func(context.Context, string) (template.HTML, error)`. With a renderer the field has two tabs, **Write** and **Preview**; opening **Preview** renders the current text in the sandboxed frame, and opening it again renders it again. Without one the field is exactly the plain editor above, and the callback is never called on save.

```go
md := goldmark.New() // defaults: raw HTML is not passed through; see below
preview := func(ctx context.Context, source string) (template.HTML, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := md.Convert([]byte(source), &out); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}

registry.Admin().Register(Profile{}, admin.Options{
	Widgets: map[string]admin.Widget{
		"Bio": admin.Markdown(admin.WithMarkdownPreview(preview)),
	},
})
```

The preview contract:

- **Route.** Admin derives one route per model that has a preview field: `POST <model>/_preview/<Field>/`. A widget cannot register its own route. The route needs a staff session, like every admin route, and a CSRF token; the browser sends the token in an `X-CSRF-Token` header, which the shared script takes from the form. A field that is unknown, read-only, a primary key or without a preview is a 404. A wrong or missing token is 403.
- **Input.** The raw source is the request body, at most 256 KiB (413 beyond that, and the text is not kept). It must be UTF-8.
- **Deadline.** The renderer's context carries a five-second deadline. The deadline is cooperative: a renderer that ignores `ctx.Done()` keeps running, and admin does not start a goroutine to answer early. A renderer that finishes after the deadline counts as timed out.
- **Output.** At most 1 MiB of HTML. Larger output is refused with 422 before it is encoded.
- **Responses.** JSON only: `{"html": ...}` on success, `{"error": "..."}` otherwise. A renderer error is 422 with a generic message, and its detail is logged server-side through the request logger, not sent to the browser. A timeout is 504. Every JSON response is `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
- **Persistence.** The route never reads or writes a row. Saving is still the normal form submit, and the textarea's value is never changed by a preview or by an error.

**The sandbox.** Admin wraps the HTML in a minimal document that carries the Content-Security-Policy meta `default-src 'none'; base-uri 'none'; form-action 'none'`. The browser assigns that document to `srcdoc` of a bare iframe with `sandbox=""` and `referrerpolicy="no-referrer"`: no scripts, no forms, no popups, no top-level navigation and an opaque origin. The policy also blocks images, styles and frames in the preview, so a preview shows the text's structure and not its outbound resources.

**The host decides sanitization.** The admin trusts the renderer's output only for the preview, which staff see inside that sandbox. A public page that shows the same text is a different trust context: the host must render it and sanitize it under its own policy. The Goldmark example above renders with defaults that do not pass raw HTML through, which is safe for the preview, but it is not a sanitization policy for public pages.

Without JavaScript the field is a plain textarea and the preview is absent. `examples/spa-accounts` edits a profile's `Bio` this way and renders nothing on its public pages.

## Branding

`admin.WithBranding` replaces the sidebar's generic title and mark with your own name and logo — the only two customization slots outside per-field widgets; there is no broader template or theme override mechanism:

```go
admin.New(store, admin.WithBranding(admin.Branding{
	Name:    "Acme Admin",
	LogoURL: "/static/logo.png",
}))
```

Omit it — `admin.New(store)` with no options — and the sidebar shows tanGO's generic default, unchanged.

## Light and dark theme

Every admin page follows the browser's light or dark preference, with no setup. Signed-in staff can also choose **Light**, **Dark** or **System** from the sidebar footer. Light and Dark are remembered in a cookie named `tango_admin_theme`, scoped to `/admin/`, set only after the staff session and CSRF token check out; System removes it again. The login page uses the cookie when there is one and the browser's preference otherwise; it has no switch. The switch is a plain form post, so it works without JavaScript.

The admin's colours are semantic tokens, not fixed palette colours, and the admin's stylesheet defines each token's light and dark value. Widgets you write should use the same token classes in the HTML they return, so they follow the theme:

| Use | Classes |
| --- | --- |
| Text | `text-ink`, `text-ink-muted`, `text-ink-faint` |
| Surfaces | `bg-surface`, `bg-surface-sunken`, `bg-surface-hover` |
| Lines | `border-line`, `border-line-subtle`, `border-control-line` (the edge of an input) |
| Accent | `bg-accent` with `text-accent-fg`, `text-accent-ui`, `ring-ring` |
| Status | `bg-success-bg text-success-fg border-success-border`, and the same for `danger`, `warning` and `neutral` |

The admin's stylesheet is precompiled and ships with the admin. Widgets can rely on the token utilities above, for the `text-`, `bg-`, `border-` and `ring-` prefixes, including their opacity variants such as `text-ink/60` and `bg-surface/50`. The opacity variants use Tailwind's default opacity steps, from `/0` to `/100`. The names in the table are covered by the compatibility promise, so renaming or removing one is a Covered change with a changelog entry and a Deprecation window. Other Tailwind utilities are not part of this contract: the stylesheet contains them only for the admin's own templates, and a release may change them. A widget that needs other styling ships its own stylesheet, served from a content-hashed URL.

A widget that hard-codes light colours stays light inside a dark admin; fixing it is the widget's job, as there is no theming hook. Branding's `LogoURL` is shown as is, so use a logo that reads on both sidebars: the light sidebar in the light theme and the dark sidebar in the dark theme.

The Markdown field's preview is deliberately a white canvas in both themes. It is an isolated, sandboxed page, so it shows what your renderer produced and is not a promise that the text matches your public site, whose styling and dark-mode behaviour are yours to own.

## Content Security Policy

The admin sends no Content-Security-Policy by default. A host runs the admin's pages under `admin.ContentSecurityPolicy()` by composing it into the admin tier:

```go
admin.New(store, admin.WithMiddleware(
	tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy())),
))
```

Under that policy the admin's own pages run no inline script or style, no inline event handler and no third-party resource. Host widgets and templates are the host's code: an inline `style` attribute or an inline event handler in a widget's markup is blocked. The [content security policy guide](content-security-policy.md#the-admin) covers logo origins, the Markdown preview and the rest.

## Operator checklist

Automated tests pin the admin's structural markup: labels, `aria-invalid` and `aria-describedby`, table captions and column scopes, the preview iframe's sandbox. They cannot see how a page behaves in a browser. Before you rely on a workflow, or after you upgrade, walk through this list in a real browser.

- **Focus order.** Tab through a create or edit form, and through a changelist with filters. Focus moves in reading order, every control shows a visible focus ring, and nothing traps it.
- **Keyboard.** Every control works without a mouse: filter selects, Search, and the **Edit**/**Formatted** and **Write**/**Preview** tabs (Left, Right, Home and End move between tabs). After a refused save, the error appears beside its field.
- **Screen reader.** A field with help text or an error reads its label, then the help text and the error. The search box and each filter announce their labels. The table announces its caption and column headers. The Markdown character count and the preview status are announced as they change.
- **Contrast.** Text, help text, errors and focus rings stay readable against their backgrounds. Check the pages as you actually deploy them, with your own styles and widgets in place.
- **Responsive layout.** Below 768px the filters wrap, each changelist row becomes a card, and the form controls stay usable.
- **JSON Formatted tab.** On a JSON field, **Formatted** shows valid JSON indented and invalid text with a visible error, and never changes the textarea unless you choose **Use this formatting**. With JavaScript off, the field is a plain textarea and the server still refuses invalid JSON.
- **Markdown Preview.** With a preview renderer set, **Preview** shows the rendered text in a frame that runs no script, loads nothing and cannot navigate the admin page. A renderer error shows in the Preview tab and never changes the text.

Recorded pass: on 2026-10-10 the maintainer checked the JSON **Formatted** and Markdown **Preview** tabs by hand in a browser against the `spa-accounts` example; they behaved as described. Repeat the checklist for your own deployment.

Known limits:

- **Display nouns are not translated.** `SingularName` and `PluralName` on `admin.Options` are plain strings, so a host's translation of a model's name is written as a literal string in the registration, not as a catalog key. The count and outcome messages around them are translatable.
- **Outcome messages are expired by display, not consumed.** The response that shows an outcome expires its cookie. The server keeps no record of used cookies, so a copy of the cookie is accepted again until its 300-second expiry, within the same session.
- **Browsers.** The Chrome smoke covers Chromium only. Firefox and Safari have not been verified.
- **Forced colours.** Windows high-contrast (forced colours) mode has not been verified.

## Stability

`Widget`, the `Options` fields this section and the one above describe, the built-in widgets other than `admin.JSON()` and `admin.Markdown(...)`, and `Branding` are **best-effort**: they are [not covered](../compatibility.md#what-is-not-covered) by tanGO's compatibility promise. Admin's internals are still expected to evolve, so this surface may change in any minor release without a Deprecation window; the changelog still lists every change.

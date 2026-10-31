# Changelog

All notable changes to tanGO are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and what each version number promises is described in [Versioning and compatibility](docs/compatibility.md).

## [Unreleased]

### Added

- **Security headers and a typed Content-Security-Policy.** `tango.SecurityHeaders` is opt-in middleware that sets a Content-Security-Policy and the companion headers `X-Content-Type-Options`, `Referrer-Policy` and `X-Frame-Options`, and sets `Strict-Transport-Security` over TLS when `tango.WithHSTS` is given. Installed with no options it applies the strict baseline, `tango.StrictCSP()`. A policy is a `tango.CSP` built from `tango.Source` values (`Self`, `None`, `Host`, `Scheme`, `Data`, `Blob`, `NonceSource`, and `UnsafeInlineStyle` for `style-src`), so there is no raw-string policy. Installs at several tiers (global, group, route, admin) compose as the guide describes: the innermost install's policy is the one sent. `tango.CSPNonce` and `tango.NonceAttr` give a template the request's nonce. `admin.ContentSecurityPolicy()` is the policy the admin's pages satisfy, composed with `admin.WithMiddleware`. `swaggerui.WithContentSecurityPolicy` serves the Swagger UI page under an application's policy, and `swaggerui.New` takes these options, so a call with no arguments is unchanged. The new API has no effect until an application installs it. See the [content security policy guide](docs/guides/content-security-policy.md).

### Changed

- **The admin phone drawer opens and closes without animation when the operating system asks for reduced motion.** The drawer's slide is replaced by an immediate change under `prefers-reduced-motion: reduce`.

- **The admin changelist row checkboxes are larger click targets.** Each row checkbox sits in a label at least 24 CSS pixels square, so a touch on a phone selects the row rather than missing it.

- **The admin's breadcrumb links and the labels beside form checkboxes are at least 24 CSS pixels tall.** Both were 20 pixels tall on a phone, below the size a touch reliably hits.

- **The admin's list, form, delete, bulk outcome, sign-in, dashboard and error pages each have their own browser tab title.** The title starts with the page's heading, such as "Crud products · tanGO Admin", and the sign-in page reads "Sign in · tanGO Admin". The bulk delete outcome page is titled with its action, "Delete selected".

- **The admin's create and edit forms explain a failed save.** A save with more than one problem shows a summary above the first field, headed with the count ("Fix 2 problems to save this book.") and listing each problem as a link to its field. Each link is the field's label followed by its message. Focus moves to the summary when the page renders; a save with one field problem moves focus to that field instead, and the summary is shown for one problem only when it is not tied to a field. Built-in widgets print each field's help text between its label and its control. A host widget keeps the help text beneath its control, unless its own markup prints it. The empty foreign key choice reads "No author selected", the add button beside a foreign key names the target in visible text ("Add new author") after a "+" that is hidden from assistive technology, and Cancel names the list it returns to.

- **Admin outcomes appear in a status region on every page.** After a create, save, delete or bulk delete, the next admin page shows the outcome in one `role="status"` region above the content, with a Dismiss control. The outcome is carried in a cookie signed with a key derived from the admin session, so it cannot be forged and is shown only to the session that set it. The page that shows the outcome expires the cookie in the browser; the server also stops accepting a copy of it 300 seconds after it was set. A copy taken before that time is still accepted until then. The bulk delete redirect no longer carries a `deleted` query parameter.

- **Admin save errors name the field and keep what you typed.** A value that is not a number, a date or valid JSON shows a plain message beside its field, and the other values in the form stay as submitted. A duplicate unique value returns 422 with a message beside the field, where it returned a server error before. A foreign key that no longer exists, and a delete the database refuses because other rows still use the row, show a plain message instead of a server error. The raw parser and database text is never shown. Messages come from `admin.*` keys with English fallbacks, so a host catalog overrides one key at a time. The sidebar, headings, breadcrumbs and messages name a model with its display noun (`admin.Options` `SingularName` and `PluralName`), not its Go type name. The style guide is `admin/MESSAGE_STYLE.md`.

- **The JSON and Markdown widgets show their editors without script, and hide what is empty.** Without JavaScript the Formatted panel of the JSON field and the Preview panel of a Markdown field are visible, stacked under the editor. The Formatted box and the Preview frame stay hidden until they have content, so the editor is the only visible control. With the script, the tabs show and the inactive panel is hidden as before.

- **The dark header is distinct from the sidebar.** The admin header has its own surface token: in the dark theme it is a lighter navy than the sidebar and the page, so the bands read apart. In the light theme the header is opaque white, where it was translucent and blurred before.

- **The widget tabs have a selection edge and a focus ring.** The selected tab is marked with a navy edge in the light theme and a cyan edge in the dark theme, and a focused tab shows a ring with an offset. Tab targets are at least 36px tall.

- **On a phone, the form's Save and Cancel buttons are full width.** Below 640px each takes the full width of the form, one above the other, with Cancel's text wrapping rather than overflowing.

- **The invalid JSON message is one sentence in the status line.** The Formatted view of an invalid document shows the sentence in the field error style, and its box stays empty.

- **The bulk delete bar states its selection, and the dialog names the count.** The bar is visible once the script runs. With no row picked, its count slot shows the reason "Select rows to delete them." and Delete selected is disabled and described by that reason; with a row picked, the count shows the selection and the button is enabled. The table's select-all is hidden on a phone, where a "Select all on this page" control in the bar takes its place. The dialog title names the count with singular and plural copy, and lists up to ten names followed by "and N more". The rejected bulk-delete page shows the section's plural name as its heading, the plain reason beneath it, and a "Back to" link. Its breadcrumb current item is "Delete selected".

- **`admin.Options` names a model's rows for messages.** The new `SingularName` and `PluralName` fields give the noun used in headings, empty states, bulk outcomes and field messages. Unset, the humanized Go name is used. Widget-internal messages stay English until a later release.

- **The admin serves a precompiled stylesheet and no longer loads Tailwind from a CDN.** The admin's stylesheet is generated by `scripts/admin-css.sh` and served from a content-hashed URL. The admin's pages no longer contain inline script, inline style or `onclick` handlers: navigation is a hashed script bound with event listeners. The admin's colour tokens and its light and dark selection are unchanged. **Upgrade note:** host widgets can rely only on the token utilities (`text-`, `bg-`, `border-` and `ring-` for the admin's token names), including their opacity variants such as `text-ink/60`, which the stylesheet styles at Tailwind's default opacity steps. Other Tailwind utilities need the host's own stylesheet: the admin's stylesheet contains them only because the admin's own templates use them, and a release may remove them. A host widget or template that needs other classes, or relied on Tailwind loading at runtime, ships its own stylesheet from a content-hashed URL. Under `admin.ContentSecurityPolicy()` a host widget's inline `style` attribute or inline event handler is blocked.

- **The admin shell is lighter and easier to use with a keyboard and on a phone.** The sidebar takes a light tinted ground in the light theme (the card surface in dark) and lists models as soft pills, the current one marked with `aria-current`. Every page starts with a skip link to the main content. Edit, delete, new and rejected bulk-delete pages show breadcrumbs. On a phone the menu opens a drawer with a Close button, Escape and the backdrop close it, focus moves into it and back, and the page behind it is inert while it is open; the drawer applies only when the admin's script runs, so a phone without script keeps the model list in the page. A new `scrim` colour token dims the page behind the drawer and the bulk delete dialog in both themes.

- **Admin error pages are HTML, and a server failure shows a short request reference.** Access denied, an expired form, a missing row, a throttled sign-in and a server failure now render an HTML page in the admin's layout, with a title, one sentence and a next step, where they returned a JSON or plain-text body. Programmatic callers of these routes that read the JSON error body now receive the HTML page. A server failure shows the first eight characters of the request's reference when the host runs `tango.RequestID` (a non-hexadecimal ID shows up to sixteen characters), and nothing technical. The full error is logged on the same line as `reference=`, so a support report can be matched to the log line. Without `tango.RequestID` the page shows no reference. A method an admin page does not handle gets the HTML page with an `Allow` header listing the methods it does handle. The access page and any failure before the account is known show no model names. A request the router refuses for an unmatched path is still answered by the router. The Markdown preview route keeps its JSON responses, and so do the 400 responses to an invalid density or theme value, which a browser form sends only when its value was edited by hand.

- **The admin changelist is denser on a desktop and reads as cards on a phone.** The table sits in one white card with a sticky header, and its numeric columns are right-aligned with tabular figures. A row's Edit and Delete links name the row for assistive technology (for example "Edit Notes on Type"). Below 768px each row becomes a card with its values labelled and its actions at the foot, so the changelist no longer scrolls sideways on a phone. A Row density control switches between comfortable (the default) and compact; it posts to `/admin/density/` with the session's CSRF token and stores the choice in the `tango_admin_density` cookie, which the server reads when it renders the page. Without script the control works as a form. **Upgrade note:** the changelist's markup and class names are not covered by the compatibility promise. Host pages that restyle the table should use the `table-admin` class and the new `cell-num`, `cell-label` and `density-bar` classes.

- **The admin changelist states its range and offers a clear action for its search and filters.** The search field is a `type="search"` input, with a Clear search link while a query is set. The applied filters are named above the table with a Clear filters link, which keeps the search. The result line reads "Showing 1 to 25 of 60 books", or "3 books" when every row fits on one page, using the model's display noun, and a filter with no choices shows no label without its control. Pagination links show a focus ring. The filter query keys and the pagination routes are unchanged.
- **`admin.Widget` takes the request context (best-effort API change; host custom widgets must adapt).** `Render` and `Parse` now receive the request's `context.Context` as their first argument, so a widget translates its own text with `i18n.T(ctx, key, fallback)` and a host catalog can word it. The built-in JSON and Markdown widgets already localised their text; they now receive the context explicitly, and the English-only widget limit is removed. `admin.Widget` is best-effort (see [what is not covered](docs/compatibility.md#what-is-not-covered)), so this is a minor-release change, not a compatibility break.

  **Migration (before and after):**

  ```go
  // before
  func (w myWidget) Render(f admin.FieldContext) template.HTML
  func (w myWidget) Parse(f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error

  // after
  func (w myWidget) Render(ctx context.Context, f admin.FieldContext) template.HTML
  func (w myWidget) Parse(ctx context.Context, f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error
  ```

  Use `_ context.Context` where the widget has no message to translate. Add `"context"` to the file's imports.

  **Compile breaks a host sees:** (1) a type stored as an `admin.Widget`, in `Options.Widgets` or returned from a function that returns `admin.Widget`, fails with "wrong type for method Render/Parse"; (2) a direct call `w.Render(f)` or `w.Parse(f, form, dest)` on any widget, including `admin.JSON()`, `admin.Markdown(...)` and `admin.Textarea()`, in host code or host tests, fails with "not enough arguments"; (3) a decorator that wraps an inner widget fails in its own methods and in the call to the inner widget; (4) a host interface that embeds `admin.Widget` or declares the two methods fails where a value is assigned to it; (5) a file that declares the new methods fails until it imports `context`. **Silent break:** a type assertion to an interface that still has the old method shape, such as `interface{ Render(admin.FieldContext) template.HTML }`, compiles and then reports false at run time; search for such assertions. `admin.PreviewWidget` and its `Preview(ctx, source)` method are unchanged.

- **Empty states name the model and say what to do next, and badges and alerts carry their state in words.** A changelist with no rows reads "No books yet." with one sentence and one "+ Add book" action. A search with no match reads "No books match “lemon”." with a Clear search action, and a filter with no match reads "No books match the selected filters." with a Clear filters action; neither says the model has no rows. The dashboard's database state and pending migrations are text badges, and an unavailable or unconfigured status or activity card shows a warning or neutral alert. Colour is never the only signal. Badges use the `badge-success`, `badge-danger`, `badge-warning` and `badge-neutral` classes; `badge-true` and `badge-false` keep their names.

- **The accounts error message takes its colour from a stylesheet.** The error paragraph on the account pages uses a class that a hashed accounts stylesheet defines (`red`, as before), instead of an inline `style` attribute. No setting changes.

- **`storage.Serve` sends a sandbox Content-Security-Policy on every object response.** Each GET and HEAD response for a stored object, including ranged and conditional responses, now carries `Content-Security-Policy: sandbox; default-src 'none'`. A browser that shows the object inline then runs it in an opaque origin with no script and no subresources, so an uploaded file cannot run script in your application's origin. Error responses from `Serve` (a missing key, a method other than GET or HEAD) carry no policy. **Upgrade note:** existing served objects now send this header, which changes how a browser displays an inline object, most visibly a PDF. Set `storage.ServeOptions{SkipSandboxPolicy: true}` on a route that must keep the previous behavior. Nothing else in the response changes. See the [uploads guide](docs/guides/uploads.md#delivering-a-file).

### Fixed

- **A foreign key field is labelled with a capitalized name when its target model has a lowercase Go type name.** The admin form and its failed-save summary showed the target's name with a lowercase first word, such as "capture Book"; it now reads "Capture Book".

- **A rejected bulk delete is titled with its outcome, and the admin's model index has a page title.** The rejection page's browser tab title reads "Nothing was deleted · tanGO Admin" instead of "Delete selected · tanGO Admin". The empty model index at `/admin/` reads "Models · tanGO Admin".

- **Dismissing the admin status notice leaves keyboard focus in the page content.** Enter on the notice's Dismiss control reloads the page, and focus previously fell to the page body. The reloaded page now focuses its content region.

### Catalog impact

This table lists every `admin.*` message key that this release renames, replaces or removes, against v0.7.0. A host catalog keeps working: a key that is removed is no longer read, and a renamed or replaced key's text falls back to English until the host translates the new key. A catalog value whose format verbs no longer match the English fallback is ignored in the same way. The admin's name is still the `admin.layout.title` key, and `admin.bulk.rejected_title` keeps its key and wording from v0.7.0. Keys and placeholders are covered API; the English wording is not.

| Old key (v0.7.0) | New key(s) | Change |
|------|------|------|
| `admin.bulk.back` | `admin.bulk.back_to` | Renamed; the placeholder is the plural noun |
| `admin.bulk.deleted_one` | `admin.bulk.outcome_one` | Renamed; the placeholder is the singular noun |
| `admin.bulk.deleted_many` | `admin.bulk.outcome_many` | Renamed; the placeholders are the count and the plural noun |
| `admin.bulk.modal_title` | `admin.bulk.title_one`, `admin.bulk.title_many` | Replaced by a singular and a plural title |
| `admin.button.confirm_delete` | `admin.delete.confirm_button`, `admin.bulk.confirm_one`, `admin.bulk.confirm_many` | Removed; each use has its own key |
| `admin.error.bulk_missing` | `admin.error.bulk_selected_missing` | Renamed; takes the singular noun |
| `admin.error.json_syntax` | `admin.error.json_position` | Renamed; the placeholder is the character position only |
| `admin.error.too_long` | `admin.error.max_length` | Renamed; the placeholders are the limit and the length. The admin no longer reads `admin.error.too_long`, so a host value for it has no effect |
| `admin.fk.add_related` | `admin.fk.add_new` | Renamed; the placeholder is the target model's singular noun |
| `admin.fk.none` | `admin.fk.none_selected` | Renamed; the placeholder is the singular noun |
| `admin.list.empty_body` | `admin.list.empty_first` | Renamed |
| `admin.list.empty_search` | `admin.list.empty_no_match` | Renamed; the plural noun, then the search text |
| `admin.list.empty_filtered` | `admin.list.empty_no_filter_match` | Renamed; the plural noun |

Removed with no replacement; the key is no longer read:

| Old key (v0.7.0) | Change |
|------|------|
| `admin.json.view_invalid` | Removed |
| `admin.json.status_invalid` | Removed |
| `admin.list.result_singular` | Removed |
| `admin.list.result_plural` | Removed |
| `admin.list.for_query` | Removed |

Keys that are new in this release, with no v0.7.0 key to replace, are listed below. Their total is 74: the `admin.*` literals in the non-test Go files of `admin/` that do not appear in the non-test `admin/` files at v0.7.0. Renamed keys also appear in the table above, under their new names.

| New key | English fallback | Arguments |
|------|------|------|
| `admin.bulk.back_to` | "Back to %s" | the plural noun |
| `admin.bulk.confirm_many` | "Delete {count} %s" | the plural noun |
| `admin.bulk.confirm_one` | "Delete 1 %s" | the singular noun |
| `admin.bulk.more_many` | "and {count} more" | the count |
| `admin.bulk.more_one` | "and 1 more" | none |
| `admin.bulk.outcome_many` | "%d %s deleted" | the count and the plural noun |
| `admin.bulk.outcome_one` | "1 %s deleted" | the singular noun |
| `admin.bulk.select_first` | "Select rows to delete them." | none |
| `admin.bulk.select_page` | "Select all on this page" | none |
| `admin.bulk.title_many` | "Delete {count} %s?" | the plural noun |
| `admin.bulk.title_one` | "Delete 1 %s?" | the singular noun |
| `admin.dashboard.status.pending_many` | "%d pending migrations" | the count |
| `admin.dashboard.status.pending_one` | "1 pending migration" | none |
| `admin.delete.confirm_button` | "Delete %s" | the singular noun |
| `admin.delete.refused` | "This %s cannot be deleted because other rows still use it. Remove or change those rows first." | the singular noun |
| `admin.density.comfortable` | "Comfortable" | none |
| `admin.density.compact` | "Compact" | none |
| `admin.density.label` | "Row density" | none |
| `admin.error.back_to_page` | "Back to the page" | none |
| `admin.error.bulk_selected_missing` | "Nothing was deleted. One selected %s no longer exists." | the singular noun |
| `admin.error.csrf_next` | "Reload the page and submit the form again." | none |
| `admin.error.csrf_title` | "Your form expired" | none |
| `admin.error.foreign_key` | "The item you chose no longer exists. Choose another and save again." | none |
| `admin.error.home` | "Back to the admin home" | none |
| `admin.error.invalid_density` | "invalid density" | none |
| `admin.error.json_position` | "This is not valid JSON at character %d. Check that commas, quotes and brackets are in place." | the character position |
| `admin.error.max_length` | "Use at most %d characters. You entered %d." | the limit and the length |
| `admin.error.method_not_allowed_next` | "Go back and use the button on the page instead." | none |
| `admin.error.method_not_allowed_title` | "That action is not available" | none |
| `admin.error.not_date_time` | "Enter a date and time." | none |
| `admin.error.not_found_next` | "Go back to the admin home and find it there." | none |
| `admin.error.not_found_title` | "We could not find that page" | none |
| `admin.error.not_number` | "Enter a number." | none |
| `admin.error.not_staff_next` | "Ask an administrator to give you staff access." | none |
| `admin.error.not_staff_title` | "You do not have access" | none |
| `admin.error.not_whole_number` | "Enter a whole number." | none |
| `admin.error.reference` | "Reference: %s" | the reference |
| `admin.error.server_load` | "This page could not be loaded." | none |
| `admin.error.server_next` | "Try again in a few minutes. If it keeps happening, give the reference below to the site administrator." | none |
| `admin.error.server_save` | "Your changes were not saved." | none |
| `admin.error.server_title` | "Something went wrong" | none |
| `admin.error.sign_in` | "Sign in" | none |
| `admin.error.too_many_login_attempts_next` | "Wait a minute, then sign in again." | none |
| `admin.error.too_many_login_attempts_title` | "Too many sign-in attempts" | none |
| `admin.error.unique_field` | "Another %s already uses this value." | the singular noun |
| `admin.error.unique_form` | "Another %s already uses one of these values." | the singular noun |
| `admin.fk.add_new` | "Add new %s" | the target model's singular noun |
| `admin.fk.none_selected` | "No %s selected" | the singular noun |
| `admin.form.cancel_to` | "Cancel and return to %s" | the plural noun |
| `admin.form.problem_item` | "%s: %s" | the field label and its message |
| `admin.form.problem_one` | "Fix 1 problem to save this %s." | the singular noun |
| `admin.form.problems` | "Fix %d problems to save this %s." | the count and the singular noun |
| `admin.index.title` | "Models" | none |
| `admin.layout.breadcrumb` | "Breadcrumb" | none |
| `admin.layout.close_menu` | "Close menu" | none |
| `admin.layout.skip` | "Skip to content" | none |
| `admin.list.clear_filters` | "Clear filters" | none |
| `admin.list.clear_search` | "Clear search" | none |
| `admin.list.count_many` | "%d %s" | the count and the plural noun |
| `admin.list.count_one` | "%d %s" | the count and the singular noun |
| `admin.list.delete_row` | "Delete %s" | the row's identity |
| `admin.list.edit_row` | "Edit %s" | the row's identity |
| `admin.list.empty_first` | "Add the first %s to see it here." | the singular noun |
| `admin.list.empty_no_filter_match` | "No %s match the selected filters." | the plural noun |
| `admin.list.empty_no_match` | "No %s match “%s”." | the plural noun and the search text |
| `admin.list.empty_no_match_filtered` | "No %s match “%s” and the selected filters." | the plural noun and the search text |
| `admin.list.filter_pair` | "%s: %s" | the filter label and its value |
| `admin.list.filters_applied` | "Filters applied:" | none |
| `admin.list.showing` | "Showing %d to %d of %d %s" | the first row, the last row, the total and the plural noun |
| `admin.login.page_title` | "Sign in" | none |
| `admin.outcome.created` | "The %s was added." | the singular noun |
| `admin.outcome.deleted` | "The %s was deleted." | the singular noun |
| `admin.outcome.saved` | "The %s was saved." | the singular noun |
| `admin.status.dismiss` | "Dismiss" | none |

Same key, English wording or argument changed (placeholders unchanged unless stated):

| Key | Change |
|------|------|
| `admin.button.add_model` | "+ Add %s"; the argument is now the singular noun, not the Go type name |
| `admin.list.search_placeholder` | "Search %s…"; the argument is now the plural noun |
| `admin.create.title` | "New %s"; the argument is now the singular noun |
| `admin.edit.title` | "Edit %s"; the argument is now the singular noun |
| `admin.delete.title` | "Delete this %s"; the argument is now the singular noun |
| `admin.list.empty_title` | "No %s yet." (was "No %s rows yet"); the argument is the plural noun |
| `admin.error.invalid_json` | "This is not valid JSON. Check that commas, quotes and brackets are in place." (was "Not valid JSON.") |
| `admin.error.json_invalid` | "This is not valid JSON. Check that commas, quotes and brackets are in place." (was "Invalid JSON") |
| `admin.error.bulk_invalid` | "Nothing was deleted. The selection is not valid." (was "the selection is not valid; nothing was deleted") |
| `admin.error.csrf` | Keeps its key and placeholders; now one sentence for the page, not the one-line error |
| `admin.error.not_found` | Keeps its key and placeholders; now one sentence for the page, not the one-line error |
| `admin.error.method_not_allowed` | Keeps its key and placeholders; now one sentence for the page, not the one-line error |
| `admin.error.not_staff` | Keeps its key and placeholders; now one sentence for the page, not the one-line error |
| `admin.error.too_many_login_attempts` | Keeps its key and placeholders; now one sentence for the page, not the one-line error |

## [0.7.0] - 2026-10-25

### Added

- **Sequenced realtime frames.** A `Logic` that also implements `realtime.Framer` frames every payload admitted to each member of its room with a per-connection sequence: 1 for the join snapshot, then 2, 3 and so on, restarting at 1 on reconnect. The host owns the wire format. `Join` fails with `realtime.ErrEmptyFrame` (or the wrapped `Frame` error) when the framed snapshot is empty or fails, and the failure does not close the existing connection or admit the new peer. Admitted sends from `Snapshot` keep their sequence and are not rolled back; a send whose `Frame` errors or overflows the queue closes the existing connection, which stays closed. A `Logic` that does not implement `Framer` is unchanged. See the [realtime guide](docs/guides/realtime-websockets.md#sequenced-frames).

- **Google Cloud Storage adapter (experimental, separate module).** `storage/gcs` is a `storage.Store` on Google Cloud Storage, in its own Go module so no cloud SDK reaches a project that does not import it. `gcs.New` authenticates with application-default credentials, workload identity or explicit credentials, and `gcs.NewFromClient` takes a client the host built. Writes are create-only and atomic, and a retried write that already committed is confirmed by a per-write token, checksum and size rather than reported as a collision. Experimental: tested against an in-process emulator, not yet against a real bucket. The module is versioned and tagged separately as `storage/gcs/vA.B.C`, after the root `v0.7.0` tag it requires. See the [uploads guide](docs/guides/uploads.md#google-cloud-storage-experimental).

- **Admin bulk delete.** `admin.Options.BulkDelete` lets a staff account delete the checked rows of one changelist page after a confirmation dialog. The delete is one all-or-nothing transaction that cascades like a single-row delete, an invalid selection (empty, over a page, a wrong `count`, a repeated or non-canonical key) is rejected whole, and each committed delete logs one `admin.EventBulkDelete` (`tango.admin.bulk_delete`) event with `model`, `selected_count` (the rows selected, not the database rows a cascade deleted) and `username`. It is off by default, needs JavaScript and never spans more than a page. See [admin registration](docs/guides/admin-registration.md#bulk-delete).

- **Grouped filters.** `Query.Filter` is an optional boolean tree ANDed with `Where` and `Any`, built with `db.Cond`, `db.And`, `db.Or` and `db.Not` (the `Expr` and `ExprOp` types). A `Query` without a `Filter` produces exactly the SQL it did before. A filter holds at most `MaxFilterNodes` (100) nodes and `MaxFilterDepth` (8) levels.

- **`IN`, `IS NULL` and `IS NOT NULL`.** `OpIn` with `db.In(field, slice)` (a nil or empty list matches nothing on every dialect; at most `MaxInValues`, 1,000, values per list and `MaxQueryArgs`, 10,000, bind values per query; a zero `time.Time` is rejected), and `OpIsNull`/`OpIsNotNull` with `db.IsNull`/`db.IsNotNull`, which take no value. All three work in `Where`, `Any` and `Filter`. A foreign key left at zero is stored as `NULL`, so `OwnerID = 0` still never matches (unchanged); use `IsNull` for unset keys.

- **`Store.First` and `Store.Exists`.** `First` reads one row (primary-key order unless `OrderBy` is set, `ErrNotFound` naming the model when none, the destination untouched on any error); `Exists` reports whether any row matches. They validate and build SQL exactly as `List` and `Count` do.

- **Typed query errors.** `ErrInvalidQuery`, `ErrQueryLimit`, `*QueryError` and `*QueryLimitError`. Every malformed query is refused before any SQL runs. The existing `Where`/`Any` validation errors keep their text and now also match `ErrInvalidQuery`.

- **Locale resolvers.** `i18n.Resolver` (a locale source that also names the request headers it depends on), the helpers `FromQuery`, `FromCookie` and `FromHost`, `Chain` (the host's order, then `Accept-Language`), `FuncResolver` for a plain function, and `MiddlewareResolver` to install one. Each helper returns only a locale with a registered catalog and none sets a cookie. `Middleware` and `DefaultLocaleResolver` are unchanged.

- **`i18n/i18ntest.Isolate`.** Test support: `i18ntest.Isolate(t)` gives a test an empty i18n state (catalogs, the `OnMissing` hook and its once-state, the observed keys) and restores the previous state in `t.Cleanup`. A test that uses it must not be parallel.

- **`i18n.Keys` and `i18n.Validate`.** `T` records each (key, fallback) pair the first time it runs with a resolved locale (nothing is recorded, and nothing allocated, when no locale is set). `Keys()` lists them; `Validate()` checks every catalog against them and returns a `Report` with `Problems` (`ProblemMismatch`, `ProblemEmpty`, `ProblemConflictingFallback` when a key is seen with two fallbacks, `ProblemRegistryFull`) and the advisory, sorted `Unobserved` catalog keys. It covers only what has run in this process, and the registry holds at most 4,096 keys, so fallbacks must be stable literals.

- **`i18n.OnMissing`.** A development hook that reports a missing translation (an absent key, an empty value or a value whose format verbs do not match the fallback's) with the resolved locale and key, at most once per pair. It is `nil` by default, runs outside the package's locks, and `OnMissing(nil)` clears it.

- **The admin's remaining English is translatable.** `<html lang>` follows the selected catalog on every admin page and on the Markdown preview document (`en` when none is selected). The Markdown and JSON widgets' tab labels, buttons, character count, `aria-label`s, iframe title, script messages (`Nothing to format.`, `Preview failed.` and the rest, delivered as `data-msg-*` attributes only when a locale was resolved), and the JSON validation errors now use `admin.*` keys with the current English as fallback. An app without i18n renders the same output as before. A test keeps new literal text out of admin's templates.

- **Translatable accounts pages.** The register, login, password-reset and email-verification pages and their form errors (including the generic login error, which stays one message for every failure) go through `i18n.T` under `accounts.*` keys with the current English as fallback. A page declares `<html lang>` only when a catalog was selected, so an app without i18n renders exactly what it did before. The JSON API's `error` text stays English; clients branch on `code`.

- **Accounts emails in the request's language.** The verification and reset emails are rendered in the locale of the request that queued them (`accounts.email.*` keys, English fallback). The locale is captured when the email is queued, before any account lookup; the queued job keeps nothing else of the request. A translated body that lacks the `%s` link placeholder falls back to the English body, so the link is never dropped.

- **The `tango newproject` scaffold points at the i18n guide.** The generated middleware list carries a two-line comment saying how to opt in (`i18n.Middleware(nil)` and catalogs). The generated project is otherwise unchanged.

- **`examples/api-with-admin` in Spanish.** The example serves the admin in Spanish end to end: a complete `es` catalog, a small `es-MX` override, a `?lang=` → cookie → `Accept-Language` order and a language switcher, with a test that walks the pages in both locales and checks `OnMissing` and `Validate`. tanGO itself still ships no catalogs.

- **`examples/accounts-mail` in Spanish.** Registering, confirming an address and resetting a password run in Spanish, emails and links included, with a test that walks the flow and fails on any missing key.

### Changed

- **The i18n middleware sets `Vary` and `Content-Language`.** With `i18n.Middleware(nil)` every response gets `Vary: Accept-Language`, so a cache in front of the app no longer serves one language to another visitor; `MiddlewareResolver(Chain(...))` sends the tokens its resolvers need (`Cookie` for `FromCookie`). `Content-Language` names the selected catalog's locale, and only when a catalog was selected. Existing `Vary` values, including ones a downstream handler adds, are merged without repeating a token (ignoring case). A resolver passed as a plain function adds no `Vary` (use `FuncResolver` to declare one). Nothing changes for an app without the middleware.

- **A bad catalog value falls back instead of printing garbage.** `i18n.T` now ignores a catalog value that is empty or whose format verbs differ from the fallback's (positional verbs such as `%[2]s` count, so reordering arguments is still fine), tries the base language, and then shows the fallback. Before, `"Nuevo"` for `"New %s"` rendered `Nuevo%!(EXTRA string=Pet)` and an empty value rendered nothing. A literal `%` in a fallback must be `%%` once arguments are passed.

- **Upgrade note: literal `%` in i18n fallbacks.** The fallback is a format string whenever arguments are passed, so a bare `%` is read as a format verb and must be escaped. Change `i18n.T(ctx, "sale.label", "Save 10% on %s", item)` to `i18n.T(ctx, "sale.label", "Save 10%% on %s", item)`. Search the project's `i18n.T` calls and catalogs for `%` followed by a letter or space before relying on the new fallback rule.

### Fixed

- **Locale matching ignores case and the `-`/`_` separator.** `Accept-Language: es-mx`, `ES-MX` and `es_MX` now select a catalog registered as `es-MX` (before, `es-mx` skipped the regional catalog and `ES-MX` matched nothing, not even `es`). The locale the middleware stores is the catalog's own spelling, and catalogs registered under two spellings of one locale are merged.

## [0.6.0] - 2026-10-21

### Added

- **Admin operational dashboard (opt-in).** `admin.WithDashboard(admin.DashboardConfig{Status, Activity})` turns `/admin/` into a page with database and migration status and recent request and job activity; without it, or with neither source set, the landing page is unchanged. The status comes from a function you supply (build it from `tango.Status`), and the activity from any `observability.Snapshotter`. A failing or panicking source marks only its own card "unavailable" and logs the cause; the error text never reaches the page. New in `observability`: `Snapshotter` and `Snapshot` (a read side that is separate from `Recorder`), `NewWindow` (an opt-in in-memory collector: the last 60 minutes in one-minute buckets, HTTP requests by status class and latency, job runs by outcome, nothing per route or job, reset on restart, labelled "this instance only"), and `Multi` (feed several recorders at once). It is a starting point for "is this instance healthy right now", not a metrics store; keep an external monitoring system for history, alerts and several instances.

- **MySQL 8.4 support.** `mysql://user:pass@host:3306/db` in `TANGO_DB_DSN` selects the new `db.MySQL` dialect, with `github.com/go-sql-driver/mysql` as the driver. Migrations, `Store`, the admin, `tango newproject --dialect=mysql` and `testdb` (a fresh database per test; `TANGO_TEST_DSN=mysql://…`, `testdb.MySQLOnly`, `testdb.NotMySQL`) all work on it, and CI runs the suite on MySQL 8.4. Tables are InnoDB with the `utf8mb4_bin` collation; timestamps are `DATETIME(6)` in UTC; connections use `READ COMMITTED` unless the DSN sets another isolation level, and the default pool recycles a connection after five minutes. MariaDB is not supported. Differences to know: an unbounded `string` that is `unique` or `index`ed is `VARCHAR(255)` (a longer value returns a `*db.ValueTooLongError`), `LIKE` is case-sensitive, and a migration that fails part-way cannot roll back (the error names every applied step and writes no tracking row). See [the setup guide](docs/guides/sqlite-and-postgresql-setup.md#mysql). A project with a `shell/main.go` from before this release must copy the new `databaseLabel` from the [shell guide](docs/guides/shell.md) before using MySQL, or `tango shell` prints the DSN's password.

- **Admin dark mode.** Every admin page now follows the browser's light or dark preference, and signed-in staff can pick Light, Dark or System from the sidebar footer; the choice is kept in a `tango_admin_theme` cookie scoped to `/admin/`. The light theme is nearly unchanged: input, checkbox and secondary-button borders are a shade darker, and primary and danger buttons darken on hover, so they meet WCAG contrast. Colours are now semantic tokens (`bg-surface`, `text-ink`, `border-line`, the status family, and so on) generated from one palette table, with contrast tests over its pairs in both themes. Widgets that hard-code light colours stay light, so switch them to the token classes in [the admin guide](docs/guides/admin-registration.md#light-and-dark-theme); the Markdown preview stays a white canvas in both themes. There is nothing to configure.

### Changed

- **`AccessLogger` is free when its logger discards Info events.** It used to wrap the response and build the event's attributes for every request and only then let the logger drop it (3 allocations a request). It now asks the logger first and calls the next handler directly. A logger that logs Info behaves as before; the level is read on every request, so a `slog.LevelVar` still takes effect at once.

- **Dependencies.** `go.mod` now requires chi 5.5.0 (was 5.3.2), `golang.org/x/crypto` 0.58.0, `x/sys` 0.49.0, `x/term` 0.47.0 and `modernc.org/sqlite` 1.60.1, and adds `github.com/go-sql-driver/mysql` 1.10.1 for the MySQL dialect. A project that depends on tanGO is raised to at least these versions the next time it updates; run `go mod tidy`.

### Documentation

- **Choosing `MaxOpen`.** The configuration guide now explains how the size of the database pool shapes tail latency, with measured figures, and suggests starting near the database's CPU count.

## [0.5.0] - 2026-10-17

### Added

- **OpenAPI documents.** `openapi.New(openapi.Options{...})` in `InstalledApps` serves an OpenAPI 3.1 document of every included route at `/openapi.json` (opt-in: nothing is served otherwise). `openapi.Dev` is the same app served only when `TANGO_OPENAPI=1`, so a deployed process exposes nothing unless it sets the variable. Each route is listed with its method, path, path parameters, name and a tag from its prefix. `tango.Doc(tango.RouteDoc{...})` adds a summary, a request body, query parameters, responses and a bearer requirement, with schemas reflected from Go types (`encoding/json` tags, nested and recursive types, `varchar=n` as `maxLength`); a type can name its schema with `SchemaName()`. `tango check` fails and names a route whose documented types cannot be described. The `accounts` JSON endpoints document themselves; the admin's pages and the cookie-flow pages are hidden from the document. `RouteRegistry.Described` and `tango.DescribedRoute` expose the route table to tools. `tango newproject` now leaves a comment in `project/project.go` saying where to add it. See [OpenAPI documentation](docs/guides/openapi.md).
- **`tango openapi [-o file]`** prints or writes the document (the new `-tango-openapi` app-side flag, handled by `DispatchFlags`; `DumpOpenAPI`, `OpenAPIProvider` and `ErrNoOpenAPI`). The output is the same bytes on every run, and a failed run leaves an existing file untouched.
- **Swagger UI.** `openapi.Options{UI: swaggerui.New()}` adds a browsable page at `/docs/` from Swagger UI 5.17.14 embedded in the binary (Apache-2.0; no network request). It is the separate package `openapi/swaggerui` and adds about 1.7 MB to a binary that imports it.

### Changed

- **Database connections are reused under load.** `database/sql` keeps only 2 idle connections, so a server handling more than 2 requests at once closed and reopened a database connection on almost every request (measured: about 10,900 PostgreSQL sessions opened in one 15-second run of single-pet reads). `ServeContext` now raises the idle limit to 32 unless your app bounded the pool with `SetMaxOpenConns`. `tango.WithDBPool(tango.DBPool{...})` sets the limits explicitly; `tango.WithDBPool(tango.DBPool{})` restores the previous behavior. The number of open connections is unchanged (unlimited).

- **Foreign keys are not indexed unless you say so (guidance).** A `fk=` column is not indexed by itself, so filtering or joining on it scans the table (measured: `list` on a 10,000-row table ran at about 1,300 requests a second without the index and about 3,200 with it). Write `tango:"fk=Category,index"` on foreign keys you filter or join on. Indexing foreign keys by default is planned for a later minor release; this entry is the notice.

### Fixed

- **The admin no longer comes up silently empty.** The admin builds its pages when it is installed, so a model registered by an app listed after `admin.New` never appeared in it. `tango check` (and `-check`) now fails and names those models, the `tango newproject` scaffold says to add apps above `admin.New`, and `tango newapp` repeats it when the project installs the admin. Move `admin.New` after your apps.

## [0.4.0] - 2026-10-10

### Added

- **JSON fields.** `tango:"json"` on a `string` field declares it holds JSON text (`model.FieldMeta.JSON`). `Store.Create` and `Store.Update` refuse text that is not valid JSON with a `*db.InvalidJSONError` that matches `db.ErrInvalidJSON` and never contains the submitted text; the empty string is unset, `null` is valid, and the stored text is exactly what was given. It is ordinary `TEXT` on SQLite and PostgreSQL, so adding the tag to an existing field generates no migration and does not scan existing rows; reads are not checked. `json` cannot be combined with `varchar=n`, `text`, `pk`, `fk`, `unique` or `index`. See [ADR 0059](docs/adr/0059-json-fields-are-declared-by-tag-stored-as-exact-text-and-validated-when-the-model-is-written.md).

- **JSON fields in the admin.** A `tango:"json"` field gets `admin.JSON()` by default (Edit and Formatted tabs) with no `Options.Widgets` entry; an explicit entry still wins, and read-only and changelist rendering are unchanged. A save the store refuses for invalid JSON, including through a non-JSON widget such as `admin.Textarea()`, re-renders with a 422 and a translatable field error (`admin.error.invalid_json`, "Not valid JSON.") beside the field, keeping the text as typed. See [admin registration](docs/guides/admin-registration.md#json-fields).

- **Admin refinement.** `admin.JSON()` edits a string field as JSON (syntax checked on save, the submitted text stored exactly, a browser-side Formatted tab). `admin.Markdown(...)` edits Markdown in a monospace textarea with a character count and, when you pass `admin.WithMarkdownPreview`, a sandboxed Preview tab from your own renderer: tanGO ships no parser or sanitizer, and you decide how HTML is sanitized (ADR 0058). `Options.Filters` adds boolean and foreign-key filters to the changelist (they combine with search and ordering; an over-large foreign-key filter still applies a primary key given in the URL). `Options.Display` with `admin.DisplayFunc` and `admin.Display` renders computed changelist cells, such as a file's size and an authorized download link, without admin knowing about storage; `admin.ByteSize` and `admin.ShortHash` help. `admin.FieldError` lets a widget report an error beside its own field. Forms now render `HelpText` and mark invalid fields with `aria-invalid` and `aria-describedby`; the changelist gets a labelled search box and filters, a caption and column scopes. `Options.Labels` now also names a field's changelist column when set. See [admin registration](docs/guides/admin-registration.md) and ADRs 0057 and 0058.

- **Bounded strings.** `tango:"varchar=n"` declares a `string` field of at most `n` characters (1 to 10,485,760), and `tango:"text"` states an unbounded one explicitly; a bare `string` stays unbounded text and generates no migration. `model.FieldMeta.MaxLength` carries the limit and a malformed tag fails `Register` with `model.ErrInvalidFieldTag`. `Store.Create` and `Store.Update` refuse a longer value before any SQL with a `*db.ValueTooLongError` (`db.ErrValueTooLong`), counting runes the way PostgreSQL counts characters; SQLite does not enforce the length and raw SQL is not checked. `tango makemigrations` writes a `VARCHAR(n)` column (`Column.Length`, additive, with no `CHECK`), generates the widening changes (`varchar(n)` to `text` or a larger `varchar`) and the narrowing ones (`text` to `varchar(n)` or a smaller `varchar`), and a narrowing checks the existing data in Go first, failing with the number of rows and one offending key instead of truncating anything. A migration of narrowings is reversible; a widening makes its migration irreversible. The admin renders `maxlength` and shows a translatable per-field message (`admin.error.too_long`) for a refused save, and `admin.FieldContext` gains `MaxLength`. Built-in models are not retagged. See [models and tags](docs/guides/models-and-tags.md#bounded-strings) and [changing a field's type](docs/guides/migrations.md#changing-a-fields-type).

- **Accounts for a single-page app.** `accounts.WithJSON` mounts six bearer-only JSON endpoints under `/accounts/api/` (register, login, password reset request and confirm, verify, resend verification) for a separate client, sharing the HTML flow's rules, limiters and outbox. The host supplies the token side through `accounts.JSONAuth` (`Issue`, `Authenticate`) and `AuthGrant`, so `accounts` imports no token package; JSON mode never reads or sets a cookie and has no logout. `JSONConfig` also takes the emailed link templates `VerifyURL` and `ResetURL` (`{token}` once; prefer a fragment), an `OnRegister` hook that runs inside the account's transaction with a `Registration` (a `FieldError` or `*db.ValueTooLongError` answers `422`), and `ResolveIdentifier` so login can take a username. Requests are strict (`415`, `413`, `400 invalid_body`), errors are `{"error", "code", "fields"?}`, and every response is `no-store`. New `db.Store.InTx` runs a callback in one transaction (commit, rollback on error, rollback and re-panic on panic; nested calls join). See the [accounts guide](docs/guides/accounts.md#json-mode-for-a-single-page-app), the new [`examples/spa-accounts`](examples/spa-accounts) and ADRs 0050 and 0051.

- **Accounts answers invalid JSON as a field error.** In the JSON accounts API, a `*db.InvalidJSONError` from a registration hook's `tx.Create` (alone or joined with `errors.Join`) answers `422 invalid_field`, keyed by the Go field name with the message `must be valid JSON`, instead of a generic `500`; the document is never echoed. See the [accounts guide](docs/guides/accounts.md#json-mode-for-a-single-page-app).

- **File uploads and storage.** The new `storage` package is an explicit object store for uploaded files, used from ordinary Views: `Put` streams a file in, always sniffs its content type, enforces a required size cap and an optional allowed-types list, and returns a generated opaque key with the size and SHA-256; `Stat`, `Open` (a byte range) and `Delete` complete the `Store`. There is no `FileField`, no file model, no listing and no signed URLs: the host keeps the key and metadata in its own model and authorizes every access. `storage.Upload` reads one multipart file with bounded memory, and `storage.Serve` delivers an authorized object with ranges, conditional requests, attachment by default and `nosniff`. Backends: `storage/local` (atomic, `os.Root`-contained, for development and one host), `storage/s3` (Amazon S3, Cloudflare R2 and other S3-compatible providers, in its own Go module so no cloud SDK reaches your `go.mod`), and `storagetest` with an in-memory `Store` and the conformance suite every `Store` passes. See the [uploads guide](docs/guides/uploads.md), the new [`examples/uploads`](examples/uploads) and ADRs 0052 to 0054. Native Google Cloud Storage is a later release.

- **Caching.** The new `cache` package is a small, explicit cache for recomputable reads: a bytes-only `cache.Store` (`Get`, `Set` with a TTL of up to 30 days, `Delete`) where a miss is not an error, one portable key rule, `cache.Prefix` for namespaces and bulk invalidation by version, typed JSON helpers (`GetJSON`, `SetJSON`, `ErrCorrupt`), and the fail-open `FetchJSON` read-through that reports cache failures to an `OnError` hook and never fails the request because the cache is down. `cache/local` is a bounded in-process store (entry limit, optional byte budget, least-recently-used eviction); `cache/redis` (Redis and Valkey) and `cache/memcache` are their own Go modules, so no client library reaches your `go.mod`. `cachetest.Run` is the conformance suite every backend passes. There is no response, fragment or ORM caching, no automatic invalidation, and nothing logs. See the [caching guide](docs/guides/cache.md), the new [`examples/cache`](examples/cache) and ADRs 0055 and 0056.

### Changed

- An admin changelist `?page=` beyond the last page now shows the last page, so the rows and the pager always describe the same page (before, it showed an empty list under a pager that named the last page).

- The admin foreign key select now starts with a "— None —" choice (translation key `admin.fk.none`), and an unset foreign key shows an em dash in the changelist instead of `0`. Saving "None" stores the same unset value (NULL) as before.

- `POST /admin/logout/` now requires the session's CSRF token when the request carries a session cookie, so a cross-site form cannot sign an admin out. A missing or wrong token gets 403 and leaves the session in place. A request without a session cookie still redirects to the login page.

- The admin sidebar footer no longer says "Signed in over Basic Auth" (admin has used session logins since 0.3). It now shows "Signed in as <username>" with a **Log out** button that posts to `/admin/logout/` with the session's CSRF token. The translation key `admin.layout.signed_in` is replaced by `admin.layout.signed_in_as` and `admin.layout.logout`.

- `/accounts/login/` now always spends one password comparison, even for an unknown or inactive account, so its timing no longer says whether an account exists. Nothing else about the HTML flow changes.

## [0.3.1] - 2026-10-09

### Fixed

- `db.Query` with an `Offset` and no `Limit` failed on SQLite with a SQL syntax error (SQLite refuses `OFFSET` without `LIMIT`). `List` now skips the rows and returns the rest on both dialects. It also made `tango shell`'s `List(model, map[string]any{"offset": n})` fail the same way.

## [0.3.0] - 2026-10-09

### Added

- **`tango shell`**: a Go prompt on your project's models and database, with nothing served. It is a Yaegi interpreter (pinned at v0.16.1) run by the project's own `shell/main.go` through the new `shell` package, so the server never links it. Helpers `Models`, `Describe`, `Get`, `List`, `Count`, `Create`, `Update` and `Delete` work on any registered model addressed as `app.Model`, with plain-map queries and rows; `-c EXPR` and piped input make it scriptable; `--readonly` refuses writes; `Options.Helpers` adds your own functions as `project.Name`. At a terminal it has line editing, per-project history under your cache directory (`TANGO_SHELL_HISTORY=off` disables it), and Ctrl-C that clears the line, or ends a running evaluation with status 130. It says what Go the interpreter cannot run (`min`, `max`, `clear`, some generics, ranging over a function or an integer) and keeps the session through errors and panics. It runs your code with your project's credentials; it is not a sandbox. See the [shell guide](docs/guides/shell.md), [ADR 0048](docs/adr/0048-tango-shell-is-the-projects-own-program-on-a-pinned-yaegi.md). Adds `github.com/traefik/yaegi` to the root module; only a binary that imports `shell` links it.
- `admin.WithTrustedProxies` and `accounts.WithTrustedProxies`: behind a reverse proxy, the failed-attempt limiters count the client address your proxy saw (the rightmost `X-Forwarded-For` entry outside the networks you name) instead of the proxy's own, so users no longer share one bucket. Opt-in: without it nothing is read from `X-Forwarded-For` and behaviour is unchanged, and only requests from the named networks may supply a forwarded address. The limiter also no longer keeps an entry for an address that has no recent failure. See [behind a reverse proxy](docs/guides/running-more-than-one-instance.md#behind-a-reverse-proxy).
- [Running more than one instance](docs/guides/running-more-than-one-instance.md): what is shared across instances (sessions, accounts, tokens), what is per process (the login limiter, `ratelimit`, `realtime` rooms, the `accounts` mail outbox and cooldown) and the workarounds, with [ADR 0047](docs/adr/0047-multi-instance-state-is-not-built-yet.md) recording that shared state is not built yet and what would reopen it. No API changes.
- A [`tango tui` guide](docs/guides/tui.md), with screens checked against the code by a test, and the promise about the dashboard in [versioning and compatibility](docs/compatibility.md): the command, its terminal requirement, confirmation before database changes and delegation to the supported operations are covered; layout, wording and key bindings are not.

### Changed

- `tango newproject` writes two more directories. `project/project.go` holds `func Config(store *db.Store) tango.Config`: the installed apps, middleware and ordinary configuration that `main.go` used to build inline, so the server and the shell register the same apps from one list. `main.go` opens the database, builds the store and calls `project.Config(store)`. `shell/main.go` is the program `tango shell` runs. `tango newapp` now says to install a new app in `project/project.go` when that file exists. Existing projects keep working as servers; to get a shell they follow the [documented upgrade](docs/guides/shell.md#adding-the-shell-to-an-existing-project), a diff tested against the scaffold as it was before ([ADR 0048](docs/adr/0048-tango-shell-is-the-projects-own-program-on-a-pinned-yaegi.md)).
- The [tutorial](docs/tutorial/01-bootstrap-routing-json.md) and the `examples/board` project list their apps in `project/project.go`; the board's token service and live feed are built inside `project.Config`, which stops the process with their error message when they cannot start.
- `tango admin create` and `tango admin resetpassword` use `TANGO_ADMIN_PASSWORD` when it is set and non-empty, with no prompt. It was documented but never read. The value is never printed; the command names the variable instead.
- `tango migrate` says what it did: `applied 2 migrations: admin/0001_auto_…, blog/0002_auto_…`, or `no pending migrations`, instead of always printing `migrations applied`. A project gets the new wording when it is built against this version; the rollback line is unchanged.
- `tango newproject` ends with the real first-run order (`tango makemigrations`, `tango migrate`, `tango admin create <username>`), and the generated project prints `admin: http://localhost:8000/admin/` after `listening on` (not with `--no-admin`). Existing projects are unchanged.
- `tango tui`:
  - the greyed-out "Shell" item is gone;
  - "Apply pending migrations" and "Roll back the latest migration" grey out with a reason when status shows they cannot run (nothing to apply, nothing to roll back, or project status incomplete);
  - the confirmation prompts say what they will do.

### Security

- `ratelimit.RemoteIPKey(trustedProxies...)` could be made to give a client the key it chose. It took the leftmost `X-Forwarded-For` entry, which the client writes when the proxy appends to the header (nginx's default), and it did not check that the value was an IP address, so a client behind a trusted proxy could rotate its key to evade a limit or flood the limiter with new keys. It also fell back to `X-Real-IP`. It now reads `X-Forwarded-For` only from a trusted peer, parses every entry as an IP address, walks the list from the right skipping trusted proxies, and takes the first other address, which is the one your nearest trusted proxy saw. `X-Real-IP` is ignored, and a valid address is returned in canonical form. Unusable data falls back to the peer's address. The failed-login limiters of `admin` and `accounts` (`admin.WithTrustedProxies`, `accounts.WithTrustedProxies`) use the same resolver. This changes behaviour under the compatibility promise's security exception.
  - *Upgrade:* nothing changes without trusted proxies. With them, check that your proxy adds the address it sees to `X-Forwarded-For` (appending and replacing both work; passing the header through untouched does not). If you relied on `X-Real-IP`, make the proxy put the client address in `X-Forwarded-For`. Keys for IPv6 clients are now lower-case and compressed, and IPv4-mapped IPv6 addresses are keyed as IPv4, so a limit that was counted under the old spelling restarts once.

### Fixed

- `tango admin create` and `resetpassword` no longer show the password as you type it on a terminal. Piped input is read as before. This adds `golang.org/x/term` to the root module ([ADR 0046](docs/adr/0046-admin-password-prompt-hides-typing-with-x-term.md)).
- Ctrl-C leaves `tango tui` even while a confirmation is showing; it used to be ignored until you answered.
- Projects created by `tango newproject` now shut down gracefully: the generated `main.go` calls `tango.ServeContext` under `signal.NotifyContext` for Ctrl-C and `SIGTERM`, so in-flight requests finish and registered `Lifecycle` components stop. It called `tango.Serve`, which never reacts to a signal. Existing applications are unchanged; swap in `ServeContext` as the [application lifecycle guide](docs/guides/application-lifecycle.md) shows.
- `tango tui` no longer ends the session when an apply or rollback fails. It shows `Last action failed: …` with the exit code and the command's last error line, refreshes the status, and returns to the menu; if the refresh also fails, both errors show.
- Stopping the server started from `tango tui` with Ctrl-C or `SIGTERM` is a clean stop: tango waits for the server, prints `server stopped` and exits 0. It used to die with the signal. A server that fails, or exits with no signal, keeps its exit code.
- `tango tui` explains a status failure (run `tango check`; an older `main.go` may need `tango.DispatchFlags`) before the raw error, which is kept.

## [0.2.0] - 2026-10-08

### Changed

- Projects created by `tango newproject` opt in to everything new in this release:
  - `LoadConfigFromEnv(tango.WithPortFromEnv())`, which replaces the hardcoded `:8000` and honours `TANGO_ADDR` and `PORT`;
  - `RequestID`, `Recoverer` and `AccessLogger`;
  - a global `MaxBodySize(1 << 20)`;
  - `MiddlewareScopeAll`.

  Existing applications keep their behaviour unless they opt in.
- `accounts` models gain `Account.EmailVerifiedAt` and a new `AccountToken` model, for password reset and email verification. Every app that installs `accounts` must run `tango makemigrations` and apply the result with `-migrate`, whether or not it enables mail: the change only adds a column and a table, so it needs no `--rename` or `--allow-drop`. Existing accounts read as unverified; nothing is gated on it unless an app uses `accounts.RequireVerified`.
- The `route` attribute is now the route's registered pattern in every log and metric for a request, trailing slash included (`/items/{id}/`), under both middleware scopes. The `tango.http.access`, `tango.http.panic` and `tango.http.request_id_generation_failed` logs used to drop a trailing slash (`/items/{id}`), while `ctx.Logger()`, `tango.http.view_error` and the HTTP metrics did not, so the same request reported two routes. Dashboards or alerts that match these three events on a trimmed `route` need the registered spelling. The attribute key is unchanged.

### Added

- A `mail` package for outgoing email: `mail.Sender`, `Message` with in-memory attachments (sent as `multipart/mixed`, 10 MiB of raw attachments by default), and three senders. `NewSMTPSender`/`SMTPSenderFromEnv` (`TANGO_SMTP_URL`) always encrypt: `smtp://` requires STARTTLS, `smtps://` is TLS from the start, and `smtp+insecure://` is plaintext to a loopback host only, with an explicit port. Errors never contain credentials, recipients or messages. `WriterSender` is for development, and `mail/mailtest` is for tests. See ADR 0044 and the mail guide.
- `accounts.WithMail` turns on password reset (`/accounts/password-reset/`) and email verification (`/accounts/verify/`), and `accounts.RequireVerified` guards a View on a verified email. Reset never reveals whether an address has an account, links are single-use, hashed, tied to the address they were sent to and built on a configured `BaseURL`, and GET never uses a link. Emails go through a bounded in-memory outbox, which logs `tango.accounts.mail_failed` and `tango.accounts.mail_dropped`. Without `WithMail`, `accounts` behaves as before. See ADR 0045.
- `Config.NotFound` and `Config.MethodNotAllowed` Views replace the router's `404` and `405` responses (tanGO sets `Allow` for a `405`). The `Allow` header lists every method routed at that path, including a non-standard one the app registered. Under `MiddlewareScopeAll` they run inside global middleware and are observed; under `MiddlewareScopeRoutes` they run outside it.
- `Config.MiddlewareScope`: with `tango.MiddlewareScopeAll`, global middleware wraps the whole router, so Unmatched requests (the router's `404` and `405`) are logged, counted, recovered and body-limited, reported with route `"(unmatched)"`. Global middleware then runs before routing, so it can't read route parameters or rewrite which route answers. The default stays `MiddlewareScopeRoutes`, today's behaviour, and may become `MiddlewareScopeAll` only after a minor release of notice; set the scope explicitly to keep yours. See ADR 0043.
- `tango.LoadConfigFromEnv(tango.WithPortFromEnv())` falls back to the `PORT` variable hosting platforms set: the address is `TANGO_ADDR`, else `":"+PORT`, else `:8000`. Without the option, `LoadConfigFromEnv` is unchanged.
- `tango.MaxBodySize(n)` middleware caps request bodies: an oversized body gets `413` with `{"error":"request body too large"}`. When several limits apply, the most restrictive wins.

### Security

- `ServeContext` (and `Serve`) now drop a client that takes more than 10 seconds to send a request's headers, so a slow-header (Slowloris-style) client can't hold connections open indefinitely. This changes default behaviour under the compatibility promise's security exception. `tango.WithReadHeaderTimeout(d)` sets another bound, and `WithReadHeaderTimeout(0)` restores the old, unbounded behaviour.

### Fixed

- `tango newapp` now prints how to install the new app: the import path read from `go.mod` and the `InstalledApps` entry to add to `main.go`. A new app used to be created silently and answer 404 until wired in.
- The compatibility page promised a JSON body for every `403`, `405` and `429` from `/accounts/*`. It now promises only the JSON `accounts` sends, for a rejected CSRF token (`403`) and rate limiting (`429`, with `Retry-After`). Closed registration's `403` is an HTML page, and the router's `405` has no body. A test pins that contract.

## [0.1.0] - 2026-10-08

### Changed

- **Breaking:** the database is chosen by `TANGO_DB_DSN`'s scheme alone, and `TANGO_DB_DIALECT` is retired: setting it is now an error. `tango.LoadDBDSNFromEnv` and `tango.LoadDBDialectFromEnv` are replaced by `tango.LoadDBConfigFromEnv`, which returns a `db.DSN` carrying the dialect, driver name and connection string together.
  - *Upgrade:* put the scheme in the DSN (`TANGO_DB_DSN=postgres://…`, or `sqlite://app.db`; unset still means `sqlite://app.db`), drop `TANGO_DB_DIALECT`, and replace the two loader calls with `dsn, err := tango.LoadDBConfigFromEnv()` followed by `sql.Open(dsn.Driver, dsn.Source)` and `dsn.Dialect`. Projects generated by `tango newproject` from this release on already do this.
- **Breaking:** `tango makemigrations` refuses to write a migration that drops a table or column unless each drop is authorised with `--allow-drop app.Model` or `--allow-drop app.Model.Field`, and refuses model changes no migration can express (a narrowing type change, a change of primary key or of a foreign key's target). A refused run writes nothing and prints the commands to choose between.
  - *Upgrade:* if a run is refused, declare a rename with `--rename` or authorise the drop with `--allow-drop`, as the error message shows.
- **Breaking:** `migration.Diff` and `migration.DiffModels` return an error alongside the migrations. These are part of the CLI-internal migration surface, which applications don't normally call.
  - *Upgrade:* handle the second return value.
- SQLite writers wait up to five seconds for a held lock instead of failing at once with `database is locked`.
- **Breaking:** `realtime/websocket.View` gained a variadic `...ViewOption` parameter. Calls compile unchanged, but code that stores `View` in a variable of its old function type no longer compiles.
  - *Upgrade:* add `...websocket.ViewOption` to that variable's function type, or call `View` directly.

### Added

- `db.ParseDSN`, which parses a scheme-qualified DSN into a `db.DSN`.
- Renaming a field or a model with `tango makemigrations --rename`, which keeps the column or table and its rows.
- Widening type changes (`integer` to `real` or `text`, `real` to `text`, `boolean` to `integer` or `text`) through the new `AlterColumnType` step.
- The public `testdb` package: an app's own tests get a fresh database for the run's Test dialect, chosen by `TANGO_TEST_DSN`.
- `examples/board`, the tutorial's app, tested end to end on SQLite and PostgreSQL.
- `realtime/websocket.View` takes options: `WithOriginPatterns` and `WithInsecureSkipVerify` configure the upgrade's origin check.

### Fixed

- Dropping a column on SQLite keeps everything else about the table: rows, indexes, unique constraints, foreign keys in both directions, defaults, `NOT NULL` and the primary key.
- `NULL` reads as the zero value, and an unset foreign key is written as `NULL`.
- Cookies are marked `Secure` behind a TLS-terminating proxy that sets `X-Forwarded-Proto`.
- Application sessions work on PostgreSQL.
- Times read back in UTC on every dialect.
- Generated migrations order their steps, and migrations across apps, by foreign key, so they apply on PostgreSQL.
- Migration files generated by v0.0.1 and v0.0.2 apply and roll back on PostgreSQL even when a table references one whose name sorts after it: the runner now creates referenced tables first and drops referencing tables first within a migration.
- Cascade delete handles circular foreign key references.
- Every SQL identifier tanGO generates is quoted, so a model named after a reserved word (`User`, `Order`) works on both dialects.
- Creating a row with an explicit ID on PostgreSQL advances the ID sequence.
- `tango newproject` keeps its standard-library imports in one sorted group.

## [0.0.2] - 2026-09-23

The `v0.0.2` tag on GitHub was later moved to a commit adding `realtime/websocket.View` options; the Go module proxy, and so every `go get`, serves the original commit (`4fd6667`), which this section and its comparison link describe. Those options are listed under 0.1.0.

### Added

- The `observability` package, with a `Recorder` interface for metrics and stable event and metric names.
- Structured HTTP logging: the `RequestID`, `Recoverer` and `AccessLogger` middleware, and HTTP duration metrics.
- Background jobs: registration, scheduling and structured failure reporting.
- `ServeContext`, with lifecycle-aware graceful shutdown, and `Lifecycle` registration on `Registry`.
- The `ratelimit` package: a token-bucket `Limiter`, `KeyFunc`, `RemoteIPKey` and `Middleware`.
- `realtime` Hub operations report to the shared `Recorder`.

### Changed

- `Serve` delegates to `ServeContext`.

### Fixed

- Every `Lifecycle.Stop` call shares one stop-phase budget.
- `RemoteIPKey` handles nil trusted CIDRs.
- `ratelimit.Limiter.Take` rejects costs a bucket can never hold.
- `Limiter` cleans up idle buckets without overflowing.

## [0.0.1] - 2026-09-21

### Added

- The first public release: the `tango` package (apps, models, routing with reverse lookup, Views, middleware, checks), `db.Store` on SQLite and PostgreSQL, schema migrations generated by `tango makemigrations`, and the `tango` CLI (`newproject`, `newapp`, `run`, `check`, `migrate`, `tui`, `admin`).
- The `admin` app: a default theme, session-cookie login with CSRF protection and rate-limited attempts, staff and superuser tiers, foreign key selects, and best-effort widgets and branding.
- Many-to-one relationships, cascade delete, and bounded `Where`/`Any`/`Count` query filtering.
- Reusable apps that ship their own migrations and static assets.
- The `auth` package for application sessions, the `accounts` app for registration, login and logout, and `auth/jwt` for HS256 bearer tokens.
- The `i18n` package.
- The `realtime` package, with single-owner rooms and generation-safe timers, and its `realtime/websocket` adapter.

[Unreleased]: https://github.com/angvp/tango/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/angvp/tango/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/angvp/tango/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/angvp/tango/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/angvp/tango/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/angvp/tango/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/angvp/tango/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/angvp/tango/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/angvp/tango/compare/4fd666747631ada5854daa820c43fb03f80851f3...v0.1.0
[0.0.2]: https://github.com/angvp/tango/compare/v0.0.1...4fd666747631ada5854daa820c43fb03f80851f3
[0.0.1]: https://github.com/angvp/tango/releases/tag/v0.0.1

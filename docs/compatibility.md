# Versioning and compatibility

This page says what a tanGO release promises not to break, and how a change that does break something is announced. It applies from v0.1.0 on. Upgrade notes for the next release are in [upgrading](upgrading.md).

## What a version number means

- **Patch releases (`0.1.x`)** never intentionally break the Covered API. Upgrading within a minor version is always safe.
- **Minor releases (`0.x.0`)** may break the Covered API, but only after a Deprecation window (below).
- **Security fixes** are the one exception: a security fix may break the Covered API in any release, and its release notes say so explicitly.

tanGO makes no v1.0 stability promise yet.

## The Covered API

Everything below is covered unless it is named under [what is not covered](#what-is-not-covered).

- **Every exported identifier of every public package**: the root `tango` package, `accounts`, `admin`, `auth`, `auth/jwt`, `db`, `i18n`, `i18n/i18ntest`, `mail`, `mail/mailtest`, `migration`, `model`, `observability`, `ratelimit`, `realtime`, `realtime/websocket`, `shell` and `testdb`. Exported means types, functions, methods, constants, variables, and the exported fields of exported structs.
- **Every `tango` CLI command** (`newproject`, `newapp`, `run`, `check`, `makemigrations`, `openapi`, `migrate`, `migrate down`, `admin …`, `tui` and `shell`; `tango help` lists them), with its flags.
- **The app-side flags** a generated `main.go` dispatches, with their JSON output and exit-code expectations:
  - `-check` validates registration, route compilation, and app-contributed checks, then exits non-zero on failure.
  - `-tango-dump-models` prints registered model metadata as JSON for `tango makemigrations`.
  - `-tango-status` prints registration/database/migration status as JSON for `tango tui`; it is read-only and does not create `tango_migrations`.
  - `-tango-openapi` prints the OpenAPI document of the installed `openapi` app (behind `tango openapi`), or fails naming `openapi.New` when none is installed.
  - `-migrate` applies pending migrations; `-migrate -down` rolls back the most recently applied one.
  - the `-tango-admin-*` flags (behind `tango admin create`, `resetpassword`, `deactivate`, `grant-staff`, `revoke-staff`, `grant-superuser` and `revoke-superuser`) manage Admin accounts when the admin app is installed.
- **`tango shell`**, as the [shell guide](guides/shell.md) describes it:
  - the command, its `-c EXPR`, `--readonly` and `--help` options, and its exit codes: `0` for a session that ends cleanly, `1` for the first error of a non-interactive session or a project that fails to boot, `2` for a command line it does not understand, and `130` when Ctrl-C ends a running evaluation;
  - `shell.Run` and `shell.Options` (`ReadOnly`, `Helpers` and `DatabaseLabel`, a display-only label the project derives from its database configuration after removing every credential) — the whole public surface of the `shell` package;
  - the eight helpers `Models`, `Describe`, `Get`, `List`, `Count`, `Create`, `Update` and `Delete`, and `Context()`, with their documented arguments: models addressed as `app.Model`, a query that is a `map[string]any` with `where` (equality only), `order`, `limit` and `offset`, and rows that are `map[string]any` keyed by Go field name holding the stored Go values;
  - `help()`, `exit()` and `quit()`, the `project.` qualifier through which `Options.Helpers` are called, and `TANGO_SHELL_HISTORY=off`.
- **Environment variables**: `TANGO_DB_DSN` (and its `sqlite://`/`postgres://`/`mysql://` grammar), `TANGO_ADDR`, `TANGO_TEST_DSN`, `TANGO_SHELL_HISTORY` (`off` keeps the shell's history from being saved) and `TANGO_ADMIN_PASSWORD` (the password for `tango admin create` and `resetpassword`; empty counts as unset).
- **`storage`**: `Store` (`Put`, `Stat`, `Open`, `Delete`), `PutOptions`, `Object`, `Info`, `NewKey`, `ValidKey`, `ETagFor`, `Inspect` and `Inspector`, `Upload` with `UploadOptions` and `UploadedFile`, `Serve` with `ServeOptions`, and the sentinel errors `ErrNotFound`, `ErrExists`, `ErrTooLarge`, `ErrTypeNotAllowed`, `ErrInvalidKey`, `ErrInvalidRange`, `ErrInvalidOptions`, `ErrNotMultipart`, `ErrNoFile`, `ErrMultipleFiles` and `ErrTooManyParts` with `TooLargeError` and `TypeNotAllowedError`; `storage/local`'s `New`; `storagetest.Run`, `Factory` and `NewMemory`; and, in its own module under the same policy, `storage/s3`'s `New`, `Config` and `Credentials`. What `Serve` sends (the headers, including the default sandbox `Content-Security-Policy` and its `ServeOptions.SkipSandboxPolicy` opt-out, the attachment default, `nosniff`, the status codes) and the rules that a key is generated and a content type is sniffed are covered; the layout of the files `storage/local` keeps on disk is not. `storage/gcs` is not covered by this promise: it is experimental and lives in its own module, released as `storage/gcs/v0.1.0` (see the [uploads guide](guides/uploads.md#google-cloud-storage-experimental)).
- **`cache`**: `Store` (`Get`, `Set`, `Delete`) and its miss-is-not-an-error semantics, `ValidKey`, `CheckTTL`, `MaxKeyLength`, `MaxTTL`, `Prefix`, `GetJSON`, `SetJSON`, `FetchJSON` with `FetchOption`, `OnError` and the `Op…` constants, the sentinel errors `ErrInvalidKey`, `ErrInvalidTTL`, `ErrTooLarge` and `ErrCorrupt` with `CorruptError`, `cache/local`'s `New` and `Config`, `cachetest.Run` and `Factory`, and, in their own modules under the same policy, `cache/redis`'s `New`, `NewFromClient` and `Config` and `cache/memcache`'s `New` and `Config`. `FetchJSON`'s fail-open rules are covered. The stored byte format and the key layout are the host's and not covered, and which Redis, Valkey and Memcached versions are supported is stated in the guide and changes as CI does.
- **OpenAPI documents**: `tango.Doc`, `tango.RouteDoc` (its fields and their meaning), `tango.DescribedRoute`, `RouteRegistry.Described`, `openapi.New`, `openapi.Dev`, `openapi.EnvVar`, `openapi.Options`, `openapi.Document`, `openapi.UI` and `swaggerui.New`; the document's structure for routes, path and query parameters, bodies, responses, `components/schemas` and the bearer scheme; and the `accounts` JSON endpoints' described shapes. A minor release may add members to the document and to the schemas. Not covered: the exact schema names of types you do not name yourself, Swagger UI's look and the version of its assets, and the operation ids of unnamed routes.
- **Query filtering and single-row reads**: `db.Expr`, `ExprOp` with `ExprCond`, `ExprAnd`, `ExprOr` and `ExprNot`, `Cond`, `And`, `Or`, `Not`, `In`, `IsNull`, `IsNotNull`, `Query.Filter`, `OpIn`, `OpIsNull`, `OpIsNotNull`, `Store.First`, `Store.Exists`, `ErrInvalidQuery`, `ErrQueryLimit`, `QueryError`, `QueryLimitError`, `MaxInValues`, `MaxFilterNodes`, `MaxFilterDepth` and `MaxQueryArgs`. Covered: a `Query` without `Filter` produces the SQL it always did; a nil or empty `IN` list matches nothing; `IsNull` finds an unset foreign key (a zero foreign key is stored as `NULL`, so `= 0` never matches); malformed queries return errors matching `ErrInvalidQuery` with text starting `tango db: invalid query: `, and the existing `Where`/`Any` errors keep their text and also match it. Not covered: the wording after that prefix for new errors, and the generated SQL text itself (its meaning is covered).
- **The `tango:"…"` struct tag grammar** described in [models and tags](guides/models-and-tags.md).
- **Bounded strings**: the `varchar=n` and `text` tags, `model.FieldMeta.MaxLength`, `model.MaxVarcharLength`, `model.ErrInvalidFieldTag`, `db.ErrValueTooLong` and `db.ValueTooLongError`, the rune-counting rule, and the additive `Length`, `FromLength` and `ToLength` fields of generated migration steps. A bare `string` field and a `tango:"text"` field never change: they stay unbounded text, generate no migration and keep today's SQL on every v0.x release.
- **JSON fields**: the `json` tag, `model.FieldMeta.JSON`, `db.ErrInvalidJSON` and `db.InvalidJSONError`. A JSON field is ordinary text: it adds no column type and no migration, and the generated migration-file format is unchanged. Whether a stored value is valid JSON is checked only by `Store.Create` and `Store.Update`.
- **The `tango_migrations` table**: its name and its columns (`app`, `name`, `applied_at`).
- **Security headers and the Content-Security-Policy API**: `SecurityHeaders` with `SecurityHeaderOption`, `WithContentSecurityPolicy`, `WithHSTS` and `WithReferrerPolicy`; the `CSP` type and its directive fields; `StrictCSP`; the `Source` type and its constructors `Self`, `None`, `Host`, `Scheme`, `Data`, `Blob`, `NonceSource` and `UnsafeInlineStyle`; `CSPNonce` and `NonceAttr`; `admin.ContentSecurityPolicy`; and `swaggerui.Option`, `swaggerui.WithContentSecurityPolicy` and the variadic `swaggerui.New`. Covered: which headers a configuration sends and when (the companion headers and their defaults, the `X-Frame-Options` value derived from `frame-ancestors`, HSTS only over TLS); the strict baseline when no policy is given; a supplied policy used as written; one nonce per request, generated only when the policy uses `NonceSource`, and the empty string when there is none; the `500` answer when a nonce cannot be made; the innermost install's header as the only one sent; and the three relaxations of the Swagger UI route. Not covered: the admin's and Swagger UI's markup that the policy governs. See the [content security policy guide](guides/content-security-policy.md).
- **The generic View-error response**: a View that returns an error, or panics behind `Recoverer`, answers `500` with the JSON body `{"error": "internal error"}`. Not covered: the admin's HTML routes, which answer their own failures, refused methods and expired forms with HTML pages in the admin's layout. Those pages are admin presentation; the JSON body above still applies to every other View.
- **`accounts`' HTTP endpoints**: `GET`/`POST /accounts/register/`, `GET`/`POST /accounts/login/` and `POST /accounts/logout/`; with `accounts.WithMail`, also `GET`/`POST /accounts/password-reset/`, `GET`/`POST /accounts/password-reset/confirm/`, `GET`/`POST /accounts/verify/` and `POST /accounts/verify/resend/`, with their links' `token` query parameter. On top of the endpoints, the only two JSON responses `accounts` sends:
  - a rejected or missing CSRF token on a `POST` answers `403` with a JSON object carrying an `"error"` string;
  - rate limiting answers `429` with the same JSON shape and a `Retry-After` header.

  With `accounts.WithJSON`, six more endpoints are covered: `POST /accounts/api/register/`, `login/`, `password-reset/`, `password-reset/confirm/`, `verify/` and `verify/resend/`. Their status codes, the members of their success bodies and the `code` values of their errors (`{"error", "code", "fields"?}`) are covered; the `error` text is not. Responses are additive-only: a minor release may add a member, so a client ignores members it does not know. Requests are strict: unknown or duplicate members are rejected with `400 invalid_body`, so a new request member is never silently accepted. Every JSON response carries `Cache-Control: no-store`. The covered Go API is `WithJSON`, `JSONConfig`, `JSONAuth`, `AuthGrant`, `Registration` and `FieldError`, and `db.Store.InTx`.

  Everything else `accounts` answers is an HTML page and not covered beyond its status: closed registration, for example, is a `403` HTML page, not JSON (`POST /accounts/api/register/` answers `403` with `code: "registration_closed"`). A request with a method an endpoint doesn't register gets the router's bare `405`, which isn't part of `accounts`' contract.
- **`mail`'s wire format**: a message with attachments is `multipart/mixed`, with the text part first and each attachment base64-encoded; the text is quoted-printable UTF-8. Exact MIME boundaries and header order are not covered.
- **Activity read side**: `observability.Snapshotter`, `Snapshot`, `RequestSummary`, `JobSummary`, `Window`, `NewWindow`, `WithClock` and `Multi`, with the documented limits of `Window` (60 one-minute buckets, global aggregates, no route, method, status code or job name retained). The `admin` dashboard's types (`DashboardConfig`, `DashboardStatus` and its state constants, `WithDashboard`) are covered the same way; how the dashboard page looks is best-effort like the rest of admin's rendering.
- **Observability names**, as listed in the [observability guide](guides/observability.md#stable-events-and-metrics):
  - the log event names `tango.http.view_error`, `tango.http.panic`, `tango.http.access`, `tango.scheduler.job_failed`, `tango.http.request_id_generation_failed`, and `accounts`' `tango.accounts.mail_failed` and `tango.accounts.mail_dropped`, and `admin`'s `tango.admin.bulk_delete`;
  - the metric names `tango_http_request_duration_seconds`, `tango_scheduler_job_invocations_total` and `tango_realtime_room_events_total`;
  - their attribute keys: `route`, `method`, `request_id`, `status`, `duration_seconds`, `error`, `recovered`, `stack`, `job`, `panicked`, `event`, `outcome` and `purpose`;
  - their enumerated values: a scheduler job's `outcome` is `success`, `error`, `canceled` or `panic`; a realtime room event's `event` is `join`, `leave` or `dispatch`, and its `outcome` is `success` or `error`; an `accounts` mail event's `purpose` is `password_reset` or `email_verification`;
  - the `route` value `"(unmatched)"`, which every Unmatched request reports under `MiddlewareScopeAll`.

A new release may add to any of these: a new function, a new optional flag, a new JSON key, a new attribute. Code and tools that ignore what they don't recognise keep working.

### Build structs with keyed literals

Keyed struct literals are the supported way to build tanGO's structs:

```go
config := tango.Config{InstalledApps: apps, Addr: ":8000"} // supported
config := tango.Config{apps, ":8000", nil}                 // unsupported
```

A minor release may add a field to any covered struct, which breaks an unkeyed literal. This is the rule Go's own compatibility promise uses. `go vet` reports unkeyed literals of imported struct types by default.

## What is not covered

- **`internal/`**. Go already prevents importing it.
- **The migration types and functions that exist for generated files and the CLI**, not for hand-written application code:
  - `migration.Step`, `migration.Column`, and the step structs `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, `DropIndex`, `RenameColumn`, `RenameTable` and `AlterColumnType`;
  - `migration.Model`, `SchemaState`, `TableState` and `ColumnState`;
  - the diff and replay surface: `Diff`, `DiffModels`, `Replay`, `Rename`, `ModelsFromMeta`, `ErrInvalidRename` and `ErrUnsupportedChange`;
  - `migration.ApplyStep`.

  These may change in any minor release, except as the [Generated-file contract](#the-generated-file-contract) protects them. Applications apply migrations with `ApplyPending` and `RollbackLast`, which are covered, as are `Migration`, `EnsureTrackingTable`, `AppliedMigrations`, `IsMissingTrackingTable`, `MigrationKey`, `ErrIrreversibleMigration` and `ErrNoAppliedMigrations`.
- **Best-effort admin extensibility**: `admin.Widget`, `admin.PreviewWidget` and the types they use (`FieldContext` and its methods, `FieldValues`, `SelectOption`), the `admin.Options` presentation fields (`Widgets`, `Labels`, `HelpText`, `ReadOnly`, `FieldOrder`), the built-in `admin.Textarea`, the presentation helpers `admin.ByteSize` and `admin.ShortHash`, and `admin.Branding`/`admin.WithBranding`. Admin's rendering and theme are still settling, so these may change in any minor release without a Deprecation window, though the changelog always lists the change. The `admin.Widget` signature changes in the next release: `Render` and `Parse` take a `context.Context` first, and a host widget must adapt; the [host-widget migration section](#migrating-host-widgets) lists what else a widget needs.
- **The admin surfaces that are covered, inside the admin's otherwise best-effort area**: `admin.Options.Filters`; `admin.Options.BulkDelete` and its guarantees (staff-only, at most one page, all-or-nothing, the whole request rejected on an invalid selection, one `admin.EventBulkDelete` event per committed delete, carrying `model`, `selected_count` and `username`); `admin.Options.Display`, `admin.DisplayFunc` and `admin.Display`; `admin.JSON()`; `admin.Markdown(...)`, `admin.MarkdownOption`, `admin.WithMarkdownPreview` and `admin.MarkdownPreview`; `admin.FieldError`; and the display-noun pair `admin.Options.SingularName` and `admin.Options.PluralName`, which name one row and several rows in messages (unset, the model's humanized Go name is used). Their Go signatures and documented behaviour follow the Covered API rules. The English wording of the messages they feed is not covered; the `admin.*` keys are, as the translatable-text rule below states. What they render does not: the admin's HTML and CSS, the filter query-string keys, the bulk delete route, its form field names and its dialog, the preview route's path and the preview's JSON bodies are presentation details that may change in any minor release.
- **Admin and `accounts` presentation**: their HTML, templates, CSS (including the light and dark theme, the colour token names and the theme switch's endpoint and cookie), CSS class names, the names and URLs of the admin's scripts and stylesheet, the `id` and `aria` attributes the admin prints, the outcome cookie's format, presentation redirects, and admin URLs. A renamed model, for example, changes its admin URL.
- **`tango tui`'s screen**: its layout, wording, colours and key bindings. What is covered is that the command exists; that the interactive dashboard requires a terminal; that actions that change the database ask for explicit confirmation first; and that it runs the same supported operations as `tango run`, `tango migrate` and `tango migrate down`. The interactive dashboard requires a terminal. In non-interactive environments, `tango tui` prints status only; use the dedicated CLI commands to run the server or change migrations. See the [`tango tui` guide](guides/tui.md).
- **What `tango shell` runs on, and how it looks**: the interpreter (Yaegi, pinned at v0.16.1) and so the exact subset of Go it can run, which a release may change; the formatting of results beyond sorted map keys and one row per line; the wording of its errors, hints, help text and startup banner; where its history file lives and its format; what happens when a value of a project's own type crosses into a helper (only plain values, maps, slices and `error` are supported); and the `project/` and `shell/` files `tango newproject` writes, which are your code, not a library API.
- **The English fallback wording of translatable text.** A translation key's name and its placeholders (`%s`, `%d`, and the `{count}` and `{max}` markers in admin widget script messages) are covered: renaming a key or changing its placeholders is a breaking change, so a host's catalog keeps working. The English text itself is not, and a release may reword it. This holds for the `admin.*` and `accounts.*` keys alike. A release that renames a key or changes its placeholders says so in the changelog under **Catalog impact:**, with the old and new names.
- **The wording of error and log messages.** Match errors with `errors.Is` against the exported sentinel errors, which are covered, never against their text.

## Migrating host widgets

A host `admin.Widget` written against the previous release needs these changes. The admin's own widgets already follow them.

- **Signature.** `Render` and `Parse` take a `context.Context` first:

  ```go
  Render(ctx context.Context, f FieldContext) template.HTML
  Parse(ctx context.Context, f FieldContext, form FieldValues, dest reflect.Value) error
  ```

  Pass `ctx` to `i18n.T` for any text the widget shows, so a host catalog can word it.
- **Colour.** Use the admin's token utilities (`bg-surface`, `text-ink`, `border-line`, the status family and their opacity variants such as `text-ink/60`), not raw palette colours. A widget that hard-codes light colours stays light in the dark theme. The admin's component classes (`btn-primary`, `field-group`, `field-help`, `field-error` and the others) keep their names and may be reused for controls.
- **Strict policy.** The admin runs under a strict Content-Security-Policy. Widget markup must not carry an inline `style` attribute, an inline event handler or an inline `<script>`. A widget's script and stylesheet are files served by its app and allowed by the policy (see the [content security policy guide](guides/content-security-policy.md#the-admin)).
- **Help text.** Built-in widgets print a field's help text between its label and its control, with `id="field-NAME-help"`. A host widget that prints its own element with that `id` takes ownership of the help text, and the form does not print a second copy. A widget that does not print it leaves the form to print it under the control.
- **Control identity.** The control must carry `id="field-NAME"`, so a summary link at the top of a failed save can focus it. When `FieldContext.Error` is set, the control carries `aria-invalid="true"`, and `aria-describedby` set to `FieldContext.DescribedBy()` points at the help text and the error.
- **Errors.** Return `admin.FieldError{Message: ...}` from `Parse` to show a message beside the field. Any other `Parse` error is shown beside its own field as a generic message.

## How defaults change

A change to how an existing app behaves, such as a new default, ships as an opt-in first, and new projects from `tango newproject` opt in. An app that doesn't opt in keeps its behaviour. A default changes later only after a minor release of notice in the changelog. Where a setting's zero value means "the framework's default" (like `Config.MiddlewareScope`), setting it explicitly keeps your choice whatever the default becomes. The one exception is a security fix, which may change a default at once and is called out in the release notes. 

## Deprecation

A deprecated part of the Covered API is announced in three places:

1. a Go `// Deprecated:` comment on the identifier;
2. a **Deprecated** entry in the [changelog](../CHANGELOG.md);
3. the release notes of the release that deprecates it.

It then keeps working for at least one minor release: something deprecated in 0.2 is removed no earlier than 0.3. A behaviour or flag change with no Go symbol to mark is announced in the changelog one minor release before it lands.

## The Generated-file contract

A migration file written by any released `tango makemigrations` keeps working on every later v0.x release: it compiles, later `tango makemigrations` runs read its `// tango:migration-json` header, and it applies and rolls back. This holds even though the step types it uses are otherwise not covered:

- the fields of `migration.Migration` (`App`, `Name`, `Up`, `Down`, `Reversible`), of `Column`, and of every step struct the generator writes may gain new fields, but a field a released generator wrote is never removed or renamed;
- the header's kinds and keys keep their meaning.

Steps you write by hand, and field combinations the generator never writes, are not protected. tanGO's test suite checks the contract against migration files generated by every released version, on SQLite, PostgreSQL and MySQL.

## Changes to tanGO's own models

The `admin` and `accounts` models reach your database through your own `tango makemigrations`, so a change to one of them is a change to the Covered API:

- it ships only in a minor release, never a patch;
- the changelog lists it under **Changed**, with instructions to run `tango makemigrations` and apply the result;
- it is always something `tango makemigrations` generates without `--rename` or `--allow-drop`: an added field or a Widening type change;
- renaming or removing one of their fields goes through the Deprecation window: the replacement arrives in one minor release, and the original is removed no earlier than the next.

## How a release is made

Every release has a dated section in the [changelog](../CHANGELOG.md), used as its release notes, and is published only after CI passed on the exact commit being tagged. The maintainer's checklist is [RELEASING.md](../RELEASING.md).

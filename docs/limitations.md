# Limitations and compatibility

tanGO is a v0.0.1 candidate. This page is the honest summary of where it stops, so you can decide whether that's fine for your project before investing in it.

## Non-goals for v0.0.1

- **Only one relationship shape: many-to-one foreign keys.** A field like `AuthorID int64 \`tango:"fk=Author"\`` is the whole contract — see [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md), which together with schema validation, referential-integrity validation, and cascade delete makes up what tanGO calls its **minimal ORM foundations**: deliberately not a full ORM. No many-to-many, no reverse accessors (`author.Posts`), no eager/lazy loading, no automatic joins. If you need any of those, write the SQL yourself via `Store.Query`/`QueryRow` — see [where `Store` stops](guides/relationships-and-admin-foreign-keys.md#where-store-stops-and-raw-sql-begins).
- **Composite unique/index constraints aren't supported.** A field's `tango:"unique"`/`tango:"index"` tag always describes that one column alone; there is no equivalent of Django's `unique_together` (a composite constraint spanning multiple fields) yet. This is a deliberate gap, not an oversight — if it's designed later, it will be a separate, non-tag, registration-time mechanism, Django-inspired in spirit but not an extension of the `fk=`/`unique`/`index` tag grammar.
- **Admin's foreign key select has no raw-SQL escape hatch.** Rendering an FK `<select>` or a related-object label runs one query per row (N+1) — acceptable for v0.0.1's admin (already a non-optimized internal tool), but there's no way yet to hand-optimize a specific list page with a custom join.
- **Admin extensibility stops well short of Django admin.** `admin.Widget`, the `Options` presentation fields, and `admin.WithBranding` (see [admin registration](guides/admin-registration.md)) cover per-field customization and two branding slots — deliberately not inlines, admin actions, permission matrices, custom querysets, custom changelist views, or full `ModelAdmin`-style subclassing. None of those are planned for v0.0.1; each would be its own future milestone if ever built.
- **Contributed migrations require explicit concatenation, not automatic discovery.** A reusable app can ship its own `Migrations` var, generated with a throwaway harness inside its own repo; the host project's `main.go` must explicitly concatenate it with the host's own `migrations.Migrations` before calling `DispatchFlags`/`ApplyPending`/`RollbackLast` — tanGO never scans installed apps for migrations on its own. See [reusable apps](guides/reusable-apps.md).
- **No rename or type-change migration steps.** A field rename or type change shows up in a generated migration as a drop-and-add, because model metadata doesn't yet track field identity across a rename.
- **`tango shell` is not implemented.** Its direction (a Yaegi-based Go interpreter, not a subprocess-per-command or a debugger) is decided, but the command itself isn't built yet.
- **No generated documentation site.** Docs are repository Markdown plus generated Go package docs (`go doc`, pkg.go.dev). No Docusaurus/Hugo/mkdocs site for v0.0.1.
- **No `tango newproject`/`newapp` interactive wizard.** Both are non-interactive, single-shot scaffolding commands; there's no guided multi-step prompt flow.

## Security boundaries

- **Admin has a real session-cookie login, bcrypt-hashed accounts, CSRF protection on every form, and rate-limited login attempts** — but it is still an internal-tool admin, not a full production auth system. There's no account lockout (only rate limiting), no idle timeout or "remember me" (sessions have a fixed lifetime from login), no password reset via email, and no admin-UI account management (`tango admin create/resetpassword/deactivate` is CLI-only, by design — see [admin registration](guides/admin-registration.md)). It's suitable for a trusted, low-traffic internal tool behind TLS — not a public-facing admin panel; tanGO provides no network-layer protection of its own.
- **The login rate limiter is in-memory and per-process.** It resets on restart and isn't shared across multiple server instances behind a load balancer — a real but bounded gap for a single-process v0.0.1 admin tool. The same limiter, and the same bound, protects the optional `accounts` app's login and registration endpoints.
- **`accounts` (the optional first-party register/login/logout app) has no password-reset email flow and no email-verification/confirmation step at signup** — a freshly registered account is usable immediately, with no mail milestone yet to gate on. There's no dedicated CLI for managing accounts either; the generic admin CRUD is the whole v0.0.1 operational story, once you've manually registered `Account` with `admin` — see [the accounts guide](guides/accounts.md).
- **JWT access tokens are stateless and cannot be revoked before expiry.** `auth/jwt` performs no database lookup, blacklist check, or account-status check during verification. There are no refresh tokens. Choose database-backed cookie sessions when immediate logout or deactivation must invalidate credentials; otherwise keep JWT lifetimes short and treat expiry as the only containment mechanism. See [JWT authentication](guides/jwt-auth.md).
- **JWT support is HS256-only with manual, fixed-set key rotation.** There is no RS256/ES256, JWKS, external identity-provider verification, automatic rotation, or remote key loading. A deployment explicitly supplies one active signing key and any retired verification-only keys.
- **Query-string JWTs can leak through infrastructure logs.** `jwt.QueryToken` exists for transports that genuinely cannot send an `Authorization` header, but URLs may appear in browser history and server or proxy access logs. Prefer `jwt.BearerToken` whenever possible.
- **`realtime` rooms are single-process and in-memory only.** There is no distributed pub/sub and no cross-instance presence — a `Hub` behind a load balancer with more than one server process does not coordinate rooms across processes. There is also no persistence or replay of room state across a restart or past a room's eviction, and no automatic game rules, bot AI, or matchmaking — `Logic` is entirely host-written.
- **`realtime`'s only backpressure policy is closing the slow peer.** A peer whose outbound queue overflows is closed outright; there is no drop-oldest/drop-newest policy and no per-peer priority. Defaults — `DefaultReconnectWindow` (30s), `DefaultPeerQueue` (16), `DefaultRoomQueue` (64), and `realtime/websocket.DefaultMaxMessageSize` (32 KiB) — are all overridable, and hosts are expected to tune them for their own traffic shape. See [realtime and WebSockets](guides/realtime-websockets.md).
- **`Hub.Close` has no `tango.Serve` integration.** tanGO has no general application-lifecycle or graceful-shutdown mechanism at all yet (`Serve` is a bare `http.ListenAndServe` call); a host using `realtime` must wire `Hub.Close` into its own shutdown path manually.
- **View errors are never exposed to clients.** A view returning an error always produces a generic 500 JSON body; the real error is only logged server-side. This is a deliberate boundary, not a gap — but it also means you must return your own structured error responses (via `ctx.JSON`) for anything a client legitimately needs to see.
- **No built-in request-size limits.** `ratelimit` (see below) addresses request *rate*, not body size — add your own `net/http` middleware for size limits if you need them.
- **`ratelimit` is single-process, in-memory only.** No distributed/shared-state quota across multiple server processes — see [ADR 0029](adr/0029-ratelimit-is-a-concrete-token-bucket.md) for why there's no storage interface yet either. No tenant-level policy engine or adaptive throttling. It is independent of, and not a replacement for, `admin`/`accounts`' existing failed-login-attempt limiter, which counts authentication failures rather than every request. See [rate limiting](guides/rate-limiting.md).

## Dialect differences

See the [SQLite/PostgreSQL setup guide](guides/sqlite-and-postgresql-setup.md) for the full list. In short: placeholder syntax, `ALTER TABLE` behavior (SQLite rebuilds tables for drop-column/unique-constraint changes; Postgres alters directly), and timestamp column types differ. None of this should be visible in your model or migration code — only in what DDL actually runs, and in raw SQL you write yourself.

## Irreversible migrations

A migration containing `DropColumn` or `DropTable` is marked irreversible. `tango migrate down` on one fails explicitly with a clear error rather than attempting to restore data it has no way to recover. If you need to test a rollback path, do it in a disposable database before applying the same migration to data you care about.

## Stable v0.0.1 CLI app-side flags

The generated `main.go` flag-dispatch convention is a stable v0.0.1 contract:

- `-check` validates registration, route compilation, and app-contributed checks, then exits non-zero on failure.
- `-tango-dump-models` prints registered model metadata as JSON for `tango makemigrations`.
- `-tango-status` prints registration/database/migration status as JSON for `tango tui`; it is read-only and does not create `tango_migrations`.
- `-migrate` applies pending migrations.
- `-migrate -down` rolls back the most recently applied migration.
- `-tango-admin-create`/`-resetpassword`/`-deactivate` (behind `tango admin create/resetpassword/deactivate`) manage Admin accounts, when the admin app is installed.

Future breaking changes to these flag names, JSON shapes, or exit-code expectations must be called out ahead of time in "APIs still expected to change" before they land.

## APIs still expected to change before a stable release

- **CLI-internal migration shapes.** `migration.Step`/`Model`/`SchemaState` and the diff/replay helpers are exported for generated files and the `tango` CLI, not as hand-authored application APIs; see the [`migration` reference](reference.md#migration-githubcomangvptangomigration).
- **`Config`'s fields.** `InstalledApps`/`Addr` are the whole of it today; expect this to grow only when a concrete consumer forces the shape, per this project's own design principle.

## Best-effort admin extensibility

`admin.Widget`, the `admin.Options` fields it and `Labels`/`HelpText`/`ReadOnly`/`FieldOrder` live in, the built-in widgets, and `admin.Branding`/`admin.WithBranding` are **best-effort**, not one of the [stable v0.0.1 CLI app-side flags](#stable-v001-cli-app-side-flags) above. This is a different, narrower promise than "APIs still expected to change before a stable release" just above: that section names things expected to *settle* before v0.0.1 ships. This admin-extensibility surface is expected to keep evolving even after v0.0.1, because admin's internals — its rendering, its default field behaviors, its theme — aren't finished settling and are likely to keep changing as real usage surfaces gaps. Breaking changes here may land without the advance-notice process the stable CLI flags get.

## Test coverage

Root-module statement coverage is **95%+**, tracked via Codecov (see the badge on the [README](../README.md)) and regenerated with:

```sh
go run gotest.tools/gotestsum@v1.13.0 \
  --junitfile junit.xml \
  --format testname \
  -- ./... -count=1 -coverprofile=coverage.out -covermode=atomic
```

A small set of lines is deliberately never exercised by a unit test, because doing so would need a live Postgres connection or a real interactive terminal rather than a meaningful behavioral test — about 23 statements (~0.7% of the codebase):

- **`db.Store.Create`'s Postgres `RETURNING`-based insert path** (`db/store.go`) — only taken when both `dialect == db.Postgres` and the model needs a backfilled default, and only actually reachable with a live Postgres connection (the SQLite-backed test suite, which is this repo's default, never exercises it). Exercised manually via the opt-in `TANGO_TEST_POSTGRES_DSN` Postgres test tier, not via the default coverage run.
- **`cmd/tango`'s entrypoint** (`cmd/tango/main.go`) — a single `os.Exit(cli.Run(...))` line; `cli.Run`'s own dispatch logic is fully covered separately in `internal/cli`.
- **The real interactive TUI event loop** (`internal/cli/tui_dashboard.go`'s `runDashboard`, backed by `tea.Program.Run()`) and **the real-stdin interactivity check** (`internal/cli/tui.go`'s `isInteractiveTerminal`) — both require an actual terminal/TTY. `tui_dashboard.go`'s own model logic (`Update`/`View`/cursor movement/dashboard state transitions) is fully unit-tested independently of the real event loop that drives it.

A further small residual (well under 1% of the codebase) of ordinary, lower-priority gaps — mostly `database/sql` driver-failure branches (`sql.Result.RowsAffected()` erroring, `sql.Rows.Scan()`/`.Columns()` erroring, `tx.Commit()` failing) and a couple of stdlib-guaranteed-safe error checks — was deliberately not chased once the 95% target was met, per this project's own design principle against writing tests for impossible or low-value branches purely to inflate a metric.


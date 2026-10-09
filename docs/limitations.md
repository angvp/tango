# Limitations

tanGO is still early. This page is the honest summary of where it stops, so you can decide whether that's fine for your project before investing in it. What each release promises not to break, and how breaking changes are announced, is in [Versioning and compatibility](compatibility.md).

## Non-goals

- **Only one relationship shape: many-to-one foreign keys.** A field like `AuthorID int64 \`tango:"fk=Author"\`` is the whole contract — see [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md), which together with schema validation, referential-integrity validation, and cascade delete makes up what tanGO calls its **minimal ORM foundations**: deliberately not a full ORM. No many-to-many, no reverse accessors (`author.Posts`), no eager/lazy loading, no automatic joins. If you need any of those, write the SQL yourself via `Store.Query`/`QueryRow` — see [where `Store` stops](guides/relationships-and-admin-foreign-keys.md#where-store-stops-and-raw-sql-begins).
- **Composite unique/index constraints aren't supported.** A field's `tango:"unique"`/`tango:"index"` tag always describes that one column alone; there is no equivalent of Django's `unique_together` (a composite constraint spanning multiple fields) yet. This is a deliberate gap, not an oversight — if it's designed later, it will be a separate, non-tag, registration-time mechanism, Django-inspired in spirit but not an extension of the `fk=`/`unique`/`index` tag grammar.
- **Admin's foreign key select has no raw-SQL escape hatch.** Rendering an FK `<select>` or a related-object label runs one query per row (N+1) — acceptable for the admin (already a non-optimized internal tool), but there's no way yet to hand-optimize a specific list page with a custom join.
- **Admin extensibility stops well short of Django admin.** `admin.Widget`, the `Options` presentation fields, and `admin.WithBranding` (see [admin registration](guides/admin-registration.md)) cover per-field customization and two branding slots, and are best-effort (see [what is not covered](compatibility.md#what-is-not-covered)) — deliberately not inlines, admin actions, permission matrices, custom querysets, custom changelist views, or full `ModelAdmin`-style subclassing. None of those are planned; each would be its own future milestone if ever built.
- **Contributed migrations require explicit concatenation, not automatic discovery.** A reusable app can ship its own `Migrations` var, generated with a throwaway harness inside its own repo; the host project's `main.go` must explicitly concatenate it with the host's own `migrations.Migrations` before calling `DispatchFlags`/`ApplyPending`/`RollbackLast` — tanGO never scans installed apps for migrations on its own. See [reusable apps](guides/reusable-apps.md).
- **Renames must be declared, and only widening type changes are generated.** Field and model renames are supported, but only when you declare them (`tango makemigrations --rename app.Model.Field=NewField` or `--rename app.Model=NewModel`); tanGO never infers one, and an undeclared rename is refused as an unauthorised drop. A field's type can change only by a Widening type change (`integer` to `real` or `text`, `real` to `text`, `boolean` to `integer` or `text`); any other type change, a change to which field is the primary key, or to a foreign key's target, makes `tango makemigrations` refuse with an error naming each such field, and write nothing.
- **`tango shell` is not implemented.** Its direction (a Yaegi-based Go interpreter, not a subprocess-per-command or a debugger) is decided, but the command itself isn't built yet.
- **No `tango newproject`/`newapp` interactive wizard.** Both are non-interactive, single-shot scaffolding commands; there's no guided multi-step prompt flow.

## Security boundaries

- **Admin has a real session-cookie login, bcrypt-hashed accounts, CSRF protection on every form, and rate-limited login attempts** — but it is still an internal-tool admin, not a full production auth system. There's no account lockout (only rate limiting), no idle timeout or "remember me" (sessions have a fixed lifetime from login), no password reset via email, and no admin-UI account management (`tango admin create/resetpassword/deactivate` is CLI-only, by design — see [admin registration](guides/admin-registration.md)). It's suitable for a trusted, low-traffic internal tool behind TLS — not a public-facing admin panel; tanGO provides no network-layer protection of its own.
- **The login rate limiter is in-memory and per-process.** It resets on restart and isn't shared across multiple server instances behind a load balancer — a real but bounded gap for a single-process admin tool. The same limiter, and the same bound, protects the optional `accounts` app's login, registration and password-reset endpoints.
- **`accounts`' password reset and email verification are single-process and best-effort.** With `accounts.WithMail`:
  - **Queued in memory:** emails wait in an in-memory outbox. A crash loses what's queued, a full outbox drops new emails (logging `tango.accounts.mail_dropped`), and a failed send is logged, never retried.
  - **Per process:** the per-address cooldown is in-memory and per-process, like the login rate limiter.
  - **No email change:** there's no email-change flow. Whoever changes `Account.Email` must clear `EmailVerifiedAt`.
  - **Fixed wording:** emails are fixed English plain text.
  - **No CLI:** there's no dedicated CLI for managing accounts either; the generic admin CRUD is the whole operational story, once you've manually registered `Account` with `admin`.

  See [the accounts guide](guides/accounts.md#password-reset-and-email-verification).
- **`mail` sends plain text to one recipient over SMTP.**
  - **Out of scope:** HTML mail, inline images, streamed attachments, DKIM, queueing, retries and bounce handling.
  - **Attachments:** they live in memory, 10 MiB of raw bytes per message by default.
  - **Trusted certificates:** an SMTP server's certificate must chain to the system's trusted roots; there's no option for a private CA.

  See [the mail guide](guides/mail.md).
- **JWT access tokens are stateless and cannot be revoked before expiry.** `auth/jwt` performs no database lookup, blacklist check, or account-status check during verification. There are no refresh tokens. Choose database-backed cookie sessions when immediate logout or deactivation must invalidate credentials; otherwise keep JWT lifetimes short and treat expiry as the only containment mechanism. See [JWT authentication](guides/jwt-auth.md).
- **JWT support is HS256-only with manual, fixed-set key rotation.** There is no RS256/ES256, JWKS, external identity-provider verification, automatic rotation, or remote key loading. A deployment explicitly supplies one active signing key and any retired verification-only keys.
- **Query-string JWTs can leak through infrastructure logs.** `jwt.QueryToken` exists for transports that genuinely cannot send an `Authorization` header, but URLs may appear in browser history and server or proxy access logs. Prefer `jwt.BearerToken` whenever possible.
- **`realtime` rooms are single-process and in-memory only.** There is no distributed pub/sub and no cross-instance presence — a `Hub` behind a load balancer with more than one server process does not coordinate rooms across processes. There is also no persistence or replay of room state across a restart or past a room's eviction, and no automatic game rules, bot AI, or matchmaking — `Logic` is entirely host-written.
- **`realtime`'s only backpressure policy is closing the slow peer.** A peer whose outbound queue overflows is closed outright; there is no drop-oldest/drop-newest policy and no per-peer priority. Defaults — `DefaultReconnectWindow` (30s), `DefaultPeerQueue` (16), `DefaultRoomQueue` (64), and `realtime/websocket.DefaultMaxMessageSize` (32 KiB) — are all overridable, and hosts are expected to tune them for their own traffic shape. See [realtime and WebSockets](guides/realtime-websockets.md).
- **No framework-installed OS signal handling.** `tango.ServeContext` shuts down gracefully when its `ctx` is canceled, but never installs a `SIGTERM`/`SIGINT` handler itself — a host wires `signal.NotifyContext` in its own `main`, as the `main.go` that `tango newproject` generates and `examples/realtime-chat` do. `Hub.Close` (and any other component) is wired in via `Registry.RegisterLifecycle`, not a bespoke shutdown path — see [application lifecycle](guides/application-lifecycle.md).
- **No process supervisor or restart policy.** A crashed or exited process is not restarted by tanGO — that's left entirely to your process manager or container orchestrator.
- **`-tango-status` can't report whether `Lifecycle` components are running.** It runs as its own short-lived process that never starts them, so it can only truthfully report registration, database and migration state, and it does. Whether a component is running is only known inside the serving process; a live health endpoint for that is future work.
- **Graceful shutdown may take up to roughly 2x `WithShutdownTimeout`.** HTTP draining and the entire `Lifecycle.Stop` pass each get their own independent timeout budget, not one shared deadline — but within the stop phase, every `Lifecycle.Stop` call shares that one budget, not a fresh one per component. Account for this when tuning a container's overall termination grace period. If the HTTP drain itself hits its deadline, `Server.Close()` is called and the drain-timeout error is still returned (`errors.Is(err, context.DeadlineExceeded)`) even though the forced close succeeded — shutdown was not fully graceful, and in-flight requests may have been aborted. See [ADR 0031](adr/0031-shutdown-uses-two-independent-phase-timeouts-and-a-decoupled-application-context.md).
- **View errors are never exposed to clients.** A view returning an error always produces a generic 500 JSON body (a [body limit](guides/routing-and-reverse-lookup.md#request-body-limits)'s rejection gets `413` instead); the real error is only logged server-side. This is a deliberate boundary, not a gap — but it also means you must return your own structured error responses (via `ctx.JSON`) for anything a client legitimately needs to see.
- **`ratelimit` is single-process, in-memory only.** No distributed/shared-state quota across multiple server processes — see [ADR 0029](adr/0029-ratelimit-is-a-concrete-token-bucket.md) for why there's no storage interface yet either. It opportunistically forgets a key once that key's bucket has been idle long enough to have fully refilled anyway, so it doesn't retain every key ever seen forever — but this is a memory optimization, not a persistence or cross-process mechanism. No tenant-level policy engine or adaptive throttling. It is independent of, and not a replacement for, `admin`/`accounts`' existing failed-login-attempt limiter, which counts authentication failures rather than every request. See [rate limiting](guides/rate-limiting.md).

## Dialect differences

See the [SQLite/PostgreSQL setup guide](guides/sqlite-and-postgresql-setup.md) for the full list. In short: placeholder syntax, `ALTER TABLE` behavior (SQLite rebuilds a table to drop a column, keeping everything else about it; Postgres alters directly), and timestamp column types differ. None of this should be visible in your model or migration code — only in what DDL actually runs, and in raw SQL you write yourself.

## Irreversible migrations

A migration containing `DropColumn`, `DropTable` or `AlterColumnType` is marked irreversible. `tango migrate down` on one fails explicitly with a clear error rather than attempting to restore data it has no way to recover. If you need to test a rollback path, do it in a disposable database before applying the same migration to data you care about.

## Observability boundaries

- Automatic HTTP metrics and `AccessLogger` observe matched routes, including middleware short-circuits and View errors. Router-generated 404 and 405 responses are observed only under `Config.MiddlewareScope: tango.MiddlewareScopeAll`, which `tango newproject` sets. Under the default scope, they pass no global middleware and are not observed. See [middleware scope](guides/routing-and-reverse-lookup.md#middleware-scope).
- Metrics are backend-neutral only. tanGO does not ship Prometheus/OpenTelemetry exporters, dashboards, distributed tracing, DB timing, or log shipping.
- Realtime and root HTTP/scheduler recorders are configured independently to preserve the package boundary.

## Test coverage

Root-module statement coverage is **95%+**, tracked via Codecov (see the badge on the [README](../README.md)) and regenerated with:

```sh
go run gotest.tools/gotestsum@v1.13.0 \
  --junitfile junit.xml \
  --format testname \
  -- ./... -count=1 -coverprofile=coverage.out -covermode=atomic
```

A small set of lines is deliberately never exercised by a unit test, because doing so would need a live Postgres connection or a real interactive terminal rather than a meaningful behavioral test — about 23 statements (~0.7% of the codebase):

- **`db.Store.Create`'s Postgres `RETURNING`-based insert path** (`db/store.go`) — only taken when both `dialect == db.Postgres` and the model needs a backfilled default, and only actually reachable with a live Postgres connection (the SQLite-backed test suite, which is this repo's default, never exercises it). Exercised by a Postgres run of the suite (`TANGO_TEST_DSN=postgres://…`), not by the default in-memory SQLite coverage run.
- **`cmd/tango`'s entrypoint** (`cmd/tango/main.go`) — a single `os.Exit(cli.Run(...))` line; `cli.Run`'s own dispatch logic is fully covered separately in `internal/cli`.
- **The real interactive TUI event loop** (`internal/cli/tui_dashboard.go`'s `runDashboard`, backed by `tea.Program.Run()`) and **the real-stdin interactivity check** (`internal/cli/tui.go`'s `isInteractiveTerminal`) — both require an actual terminal/TTY. `tui_dashboard.go`'s own model logic (`Update`/`View`/cursor movement/dashboard state transitions) is fully unit-tested independently of the real event loop that drives it.

A further small residual (well under 1% of the codebase) of ordinary, lower-priority gaps — mostly `database/sql` driver-failure branches (`sql.Result.RowsAffected()` erroring, `sql.Rows.Scan()`/`.Columns()` erroring, `tx.Commit()` failing) and a couple of stdlib-guaranteed-safe error checks — was deliberately not chased once the 95% target was met, per this project's own design principle against writing tests for impossible or low-value branches purely to inflate a metric.

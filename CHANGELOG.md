# Changelog

All notable changes to tanGO are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and what each version number promises is described in [Versioning and compatibility](docs/compatibility.md).

## [Unreleased]

### Added

- `admin.WithTrustedProxies` and `accounts.WithTrustedProxies`: behind a reverse proxy, the failed-attempt limiters count the client address your proxy saw (the rightmost `X-Forwarded-For` entry outside the networks you name) instead of the proxy's own, so users no longer share one bucket. Opt-in: without it nothing is read from `X-Forwarded-For` and behaviour is unchanged, and only requests from the named networks may supply a forwarded address. The limiter also no longer keeps an entry for an address that has no recent failure. See [behind a reverse proxy](docs/guides/running-more-than-one-instance.md#behind-a-reverse-proxy).
- [Running more than one instance](docs/guides/running-more-than-one-instance.md): what is shared across instances (sessions, accounts, tokens), what is per process (the login limiter, `ratelimit`, `realtime` rooms, the `accounts` mail outbox and cooldown) and the workarounds, with [ADR 0047](docs/adr/0047-multi-instance-state-is-not-built-yet.md) recording that shared state is not built yet and what would reopen it. No API changes.
- A [`tango tui` guide](docs/guides/tui.md), with screens checked against the code by a test, and the promise about the dashboard in [versioning and compatibility](docs/compatibility.md): the command, its terminal requirement, confirmation before database changes and delegation to the supported operations are covered; layout, wording and key bindings are not.

### Changed

- `tango shell` is still not implemented, but its help text, `docs/limitations.md` and [ADR 0005](docs/adr/0005-yaegi-for-tango-shell.md) now say what a spike found: Yaegi works against a tanGO project, cannot run some current Go, and a shell needs a program in your own module. No command was added.
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

[Unreleased]: https://github.com/angvp/tango/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/angvp/tango/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/angvp/tango/compare/4fd666747631ada5854daa820c43fb03f80851f3...v0.1.0
[0.0.2]: https://github.com/angvp/tango/compare/v0.0.1...4fd666747631ada5854daa820c43fb03f80851f3
[0.0.1]: https://github.com/angvp/tango/releases/tag/v0.0.1

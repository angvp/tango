# Public terminology

This glossary names concepts that are easy to confuse in tanGO's public API.

## JWT claims

`auth/jwt.Claims{Subject, Issuer, Audience, IssuedAt, ExpiresAt}` is the fixed decoded payload returned by `auth/jwt.Service`. `Subject` is an opaque host-owned identity, not an `accounts.Account` or another model. Claims are stateless and have no database row behind them.

Do not call decoded claims an Application session: sessions are persisted, revocable rows managed by `auth` primitives.

## JWT signing key

`auth/jwt.Key{ID, Secret}` is one HMAC key identified by `kid`. A service has one active key for issuing new tokens and a fixed set of retired verification-only keys. The active key is automatically included in the verification set. Rotation is manual across deployments; there is no JWKS, remote loading, or automatic rotation.

## Room loop

The single goroutine that is the sole owner of one active `realtime` room's membership, timers, and every call into host-supplied `Logic` — `Join`, `Leave`, `Dispatch`, `DispatchPeer`, and timer expiry all become events processed one at a time, in arrival order, by this loop alone. No other goroutine (a socket's read loop, its writer goroutine, a timer callback, a bot's scheduler) ever mutates room state directly — even a failed outbound `Send` is reported back to the room loop as an event, not handled where it happened. See [ADR 0028](docs/adr/0028-realtime-rooms-use-a-single-owner-event-loop-not-locks.md).

Do not call this just "the room": that's ambiguous between the owning goroutine itself and the domain state `Logic` maintains inside it — say Room loop when specifically meaning the owning goroutine.

## Peer

One live, replaceable transport connection for one `Principal` in one room — not the user, and not a retained room member in its own right. Must be non-nil and comparable (a pointer type); `Join` rejects anything else outright. Joining with a new `Peer` while an old one is still live for the same room and `Principal` replaces it: the old `Peer`'s eventual `Leave` is a stale, idempotent no-op, and its `DispatchPeer` calls are rejected the same way, never reaching `Logic`. A `Peer` going away (closed, its outbound queue overflowed, or its `Send` failing) does not by itself end the underlying room membership — see Reconnect window.

Do not call this a Connection generically, or conflate it with Principal: Peer is the transport, Principal is the identity it currently carries.

## Principal

The stable, transport-independent actor identity a host supplies to `Join`/`Dispatch` — `realtime.Principal{UserID}`, deliberately just that one field. A `Principal` can act without ever holding a live `Peer` (a bot dispatches actions this way); it is `Peer` that comes and goes, never `Principal`. tanGO does not distinguish "human" from "bot" `Principal`s at the framework level — an application needing that distinction owns its own convention.

Do not tie a Principal to `accounts.Account` or `AdminUser`: a host maps its own identity concept onto `UserID` however it likes, the same way JWT claims's `Subject` is opaque.

## Reconnect window

How long a `realtime` room retains its in-memory membership/state for a `Principal` with no live `Peer`, before implicit eviction. Purely a temporary-retention mechanism, not durable persistence and not a guarantee that any particular socket stays alive — a `Principal` reconnecting within the window gets its room membership restored and receives a fresh `Logic.Snapshot`; past the window, an otherwise-empty room is evicted and any in-memory state it held is gone. Bots never hold membership, so they neither keep a room alive during this window nor are affected by it ending.

Do not call this a session: this is in-memory retention only, with no durability guarantee across a process restart or `Hub.Close`.

## Decision

`ratelimit.Decision`, the outcome of one `Limiter.Take` call — `Allowed`, `Limit`, `Remaining`, `RetryAfter`. Passed to a rejected request's `LimitedHandler` so a host can build its own response/headers from it instead of trusting `ratelimit`'s default body. Not the token bucket's internal state (token count, last-refill time) — those stay unexported inside `Limiter`.

Do not call this a Result or Verdict: say Decision specifically when meaning `ratelimit.Decision`.

## Key extractor

A `ratelimit.KeyFunc`, a caller-supplied `func(*http.Request) (string, error)` that produces the string a `Limiter` buckets by. `ratelimit` ships one default, `RemoteIPKey`, but never imports `auth`/`auth/jwt`/`accounts` itself — a host wanting to key on identity rather than IP writes its own. An extractor's error is distinct from a rejected Decision: `Middleware` routes it to `ErrorHandler`, never `LimitedHandler` — an extraction failure is not the same fact as "this caller is over budget."

Do not conflate an extractor error with a limited/rejected request: they go to different handlers for a reason.

## Lifecycle component

One `tango.Lifecycle{Name, Start, Stop}` registered via `Registry.RegisterLifecycle`, explicitly, inside an `App`'s `Register` callback — never started implicitly by registering it. `Start`/`Stop` are each independently optional (nil means no-op for that phase), but at least one must be set and `Name` must be non-empty; `RegisterLifecycle` rejects violations rather than normalizing them. Distinct from Checker (an optional-interface, one-per-App, metadata-only pattern) — see [ADR 0030](docs/adr/0030-lifecycle-is-explicit-registry-registration-not-an-optional-app-interface.md) for why Lifecycle deliberately isn't shaped the same way.

Do not conflate this with App: an App may register zero, one, or several Lifecycle components, they are not the same thing. Do not conflate it with Checker either: a different, narrower, implicit-interface mechanism.

## Application context

The long-lived `context.Context` `tango.ServeContext` passes to every `Lifecycle.Start`, and that a component may retain for its own background goroutines. Deliberately not a direct child of the caller's context passed to `ServeContext` — built via `context.WithoutCancel` plus `ServeContext`'s own cancellation. Once every component has started, component work isn't torn down the instant the caller triggers shutdown, only once HTTP draining has actually finished; during startup itself, this context *is* canceled promptly the moment the caller cancels, precisely so a `Start` cooperatively blocked on it can observe cancellation and return rather than hang. See [ADR 0031](docs/adr/0031-shutdown-uses-two-independent-phase-timeouts-and-a-decoupled-application-context.md).

Do not conflate this with Shutdown context: a different, second, phase-scoped context. Do not conflate it with the caller's own `ctx` either: a distinct value `ServeContext` deliberately decouples cancellation from (narrowly, only once startup has fully succeeded).

## Shutdown context

A fresh, timeout-bounded `context.Context` (per `WithShutdownTimeout`, default 15s) that `tango.ServeContext` creates independently for each shutdown phase — one for `http.Server.Shutdown`'s drain, a separate one later shared by the entire reverse-order `Lifecycle.Stop` pass (every `Stop` call in that pass gets the same context, not a fresh one each) — never derived from the by-then-canceled Application context. Two independent budgets, not one shared remainder: a graceful shutdown may take up to roughly twice the configured timeout in the worst case, regardless of how many `Lifecycle`s are registered. Hitting the drain phase's deadline still forces `Server.Close()` and still reports the timeout in the returned error, even though the forced close itself succeeds.

Do not conflate this with Application context: the long-lived Start-time context, never the same value as a Shutdown context, which is short-lived and created fresh per phase. Do not assume one shutdown timeout covers the whole sequence — it covers one phase at a time. Do not assume the stop-phase context resets between `Lifecycle.Stop` calls — it's one shared budget for the whole reverse-order pass.

## Test dialect

The single SQL dialect one `go test` run targets — SQLite or PostgreSQL — chosen by the environment, never by an individual test. One variable, `TANGO_TEST_DSN`, selects it by scheme, using the same grammar as the runtime `TANGO_DB_DSN`: unset or `sqlite://:memory:` is an in-memory SQLite run, any other `sqlite://` path is a file-backed SQLite run (a fresh file per test; the path itself is not used), and `postgres://…`/`postgresql://…` is a PostgreSQL run. Any other scheme is an error. A test that cannot run on one dialect says so explicitly (`testdb.SQLiteOnly` / `testdb.PostgresOnly`) with a reason. In a Postgres run, an unreachable database is a failure, not a skip.

Do not confuse this with `db.Dialect`, the value a `Store` uses to generate SQL: the Test dialect decides which `db.Dialect` every test in the run receives.

## Rename mapping

A developer's explicit statement, given when generating migrations, that a model or field now carries a new name but is the same thing: its table or column, and the rows in it, are kept and renamed rather than dropped and recreated. tanGO never infers a rename from a drop and an add that look alike; without a Rename mapping, a name that disappears from the models is a drop, and a drop needs its own explicit permission.

Do not confuse this with a drop plus an add: a drop destroys the data, a rename keeps it. Do not read a Rename mapping as touching the Go code: the developer renames the struct or field themselves; the mapping only tells the migration what happened.

## Widening type change

A change to a field's Go type whose new column type can hold every value the old one could, so existing rows convert without loss or failure: `integer` to `real` or `text`, `real` to `text`, and `boolean` to `integer` or `text`. These are the only column type changes a generated migration expresses; any other type change is refused when migrations are generated.

Do not confuse this with a narrowing change (for example `text` to `integer`), where some existing values cannot convert. Changes within one column type, such as `int` to `int64`, are not type changes at all.

## Covered API

The public surface tanGO's compatibility promise applies to. It covers:

- every exported identifier of every public package;
- every `tango` CLI command and CLI app-side flag;
- the other contracts the compatibility page lists, such as environment variables and machine-read JSON.

Some exports are excluded by name, and anything not excluded is covered. A patch release never intentionally breaks the Covered API. A minor release may break it, but only after a Deprecation window.

Do not confuse this with "exported": an exported identifier can be excluded, such as the migration step types. Something that isn't Go at all can be covered, such as `TANGO_DB_DSN`.

## Deprecation window

The period, at least one minor release long, during which a deprecated part of the Covered API keeps working before a later minor release may remove it. For example, something deprecated in 0.2 can be removed no earlier than 0.3. A deprecation is announced in the Go doc comment, the changelog and the release notes. A behaviour change with no symbol to mark is announced in the changelog one minor release ahead.

Do not confuse this with a removal: during the window the old form must still work.

## Generated-file contract

The promise that a migration file written by a released `tango makemigrations` keeps compiling on every later v0.x release. Later runs can still read its header, and it still applies and rolls back. The promise holds even though the migration step types it uses are otherwise excluded from the Covered API.

Do not confuse this with a promise about hand-written steps: only what a released generator wrote is protected.

## Unmatched request

A request that no route answers: no route matches its path (not found), or a route matches its path but not its method (method not allowed). The router answers it without running any View. Whether a request is unmatched is decided from the request as it arrived, before any middleware runs. In logs and metrics, every unmatched request is reported under one fixed route name, never the URL that was requested.

Do not confuse this with a View that answers "not found" for a route it does match, such as a lookup of a missing row: that request matched a route and is reported under that route's pattern.

## Body limit

The most bytes a request's body may carry before tanGO rejects the request as too large. When more than one body limit applies to a request, the most restrictive wins: a limit set closer to a View can keep or lower a limit set further out, never raise it. A route that must accept larger bodies is left outside the stricter limit rather than given a higher one.

Do not confuse this with rate limiting, which bounds how often a caller may make requests, not how large one request may be.

## Sender

Whatever delivers an outgoing email message on the application's behalf: an SMTP relay, a provider, or a development stand-in that only writes the message out. Sending is outbound only; a Sender never receives mail.

Do not confuse a Sender with the message's `From` address: the Sender is how a message travels, `From` is who it says it's from.

## Reset token

A secret, single-use proof that whoever holds it can choose a new password for one account. It is sent only to that account's email address, expires after a short time, and stops working once used or once a newer one is issued for the same account. Completing a reset ends every login the account had.

Do not confuse it with a session token, which proves a login already happened, or with a JWT, which can't be revoked.

## Verified email

An account whose owner has proven they receive mail at its email address, and when they did. It is information about identity, not a permission and not activity state: an unverified account can still log in, and only an application that asks for verification blocks it.

Do not confuse this with an account being active, which is an operator's decision.

## Public base URL

The absolute origin, such as `https://example.com`, that links sent outside a request are built on, for example in an email. It is configured by the application, never taken from the request that caused the email.

# Public terminology

This glossary names concepts that are easy to confuse in tanGO's public API.

## JWT claims

`auth/jwt.Claims{Subject, Issuer, Audience, IssuedAt, ExpiresAt}` is the fixed decoded payload returned by `auth/jwt.Service`. `Subject` is an opaque host-owned identity, not an `accounts.Account` or another model. Claims are stateless and have no database row behind them.

Do not call decoded claims an Application session: sessions are persisted, revocable rows managed by `auth` primitives.

## JWT signing key

`auth/jwt.Key{ID, Secret}` is one HMAC key identified by `kid`. A service has one active key for issuing new tokens and a fixed set of retired verification-only keys. The active key is automatically included in the verification set. Rotation is manual across deployments; v0.0.1 has no JWKS, remote loading, or automatic rotation.

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

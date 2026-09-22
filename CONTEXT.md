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

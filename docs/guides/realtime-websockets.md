# Realtime rooms and WebSockets

`github.com/angvp/tango/realtime` is a transport-neutral, single-process room-coordination primitive: chat rooms, turn-based games, presence — anywhere multiple connected actors need ordered, shared state. `github.com/angvp/tango/realtime/websocket` is the optional HTTP WebSocket adapter for it. Neither package is coupled to `auth`, `auth/jwt`, or `accounts`; a host supplies its own authentication callback.

The runnable reference is [`examples/realtime-chat`](../../examples/realtime-chat). It demonstrates a `Hub`, a chat `Logic`, `auth/jwt`-based authentication over a WebSocket handshake, join/leave notices, and an idle timer.

## Constructing a Hub

One `Hub` serves one room type — one `Logic` implementation, constructed per room ID by a `Factory`:

```go
hub, err := realtime.NewHub(func(roomID string) realtime.Logic {
    return chatLogic{}
}, realtime.Options{})
```

A host running more than one kind of room (chat and a game, say) constructs more than one `Hub`. `Options{ReconnectWindow, PeerQueue, RoomQueue}` all default to sane values (`DefaultReconnectWindow` 30s, `DefaultPeerQueue` 16, `DefaultRoomQueue` 64) when left zero; a negative value is rejected by `NewHub` itself, at construction, not at first use.

## The `Logic` contract

```go
type Logic interface {
    Handle(*RoomContext, Event) error
    Snapshot(*RoomContext, Principal) ([]byte, error)
}
```

`Handle` processes one `Event` at a time — `EventJoin`, `EventLeave`, `EventAction` (a dispatched client/bot action), or `EventTimer` (a fired `ResetTimer`). `Snapshot` returns the payload a `Principal` should receive immediately upon joining or reconnecting; `realtime` delivers it before any other room event reaches that peer.

`*RoomContext` is only valid inside one `Handle`/`Snapshot` call — never store it. Its methods:

```go
func (r *RoomContext) Broadcast(payload []byte) error
func (r *RoomContext) BroadcastExcept(userID string, payload []byte) error
func (r *RoomContext) SendUser(userID string, payload []byte) error
func (r *RoomContext) ResetTimer(name string, duration time.Duration) error
```

`Payload []byte` is opaque throughout `realtime` — never parsed or inspected by the framework, only by `Logic`. There is no built-in wire envelope; encoding is entirely the host's concern, the same split `db.Store`/`admin.Widget` already draw between mechanism and encoding elsewhere in tanGO.

One ordering detail worth knowing up front: `EventJoin` fires after membership is already installed, so a plain `Broadcast` on join notifies the joiner about their own join. Use `BroadcastExcept` there if that's not wanted. `EventLeave` doesn't have the same issue — membership is removed before `Handle` runs for it, so `Broadcast` already excludes the leaver.

## The single-owner room loop

Each active room is owned by exactly one goroutine, which alone applies `Join`/`Leave`/`Dispatch`, mutates membership, runs timers, and calls into `Logic`. See [ADR 0028](../adr/0028-realtime-rooms-use-a-single-owner-event-loop-not-locks.md) for the full reasoning; in practice this means:

- **`Hub.Join`/`Leave`/`Dispatch`/`DispatchPeer` block the caller** until the room loop has actually applied the operation — not until any network I/O completes, just until room state reflects it. `Join`/`Leave` also wait for `Logic.Handle`'s corresponding `EventJoin`/`EventLeave` call and return its error (membership and any snapshot delivery already applied are never rolled back on that error — same no-rollback rule as `Dispatch`, below). A stale `Leave` (a `Peer` no longer current) is an idempotent no-op that never reaches `Logic.Handle` at all. `Dispatch`/`DispatchPeer` return `Logic.Handle`'s own error too, so a caller (including a bot's own scheduler) can observe domain rejection, not merely "enqueued OK."
- **Peer identity is validated once, at `Join`.** A nil `Peer` — including a typed-nil pointer hiding inside a non-nil `Peer` interface value, which is comparable and `!= nil` as an interface but can still panic if its methods are called — or one whose dynamic type is not comparable, is rejected there with `ErrInvalidPeer` before it can ever be admitted into membership. This is what makes every later identity comparison (a stale `Leave`, `DispatchPeer`'s generation check) safe regardless of what concrete `Peer` type a host passes.
- **Peer replacement is generation-safe.** Joining with a new `Peer` for a `(RoomID, Principal)` that already has a live one replaces it; the old `Peer`'s eventual `Leave` — its read loop finally noticing the close — is recognized as stale and is a no-op, never removing the replacement. The same generation check gates `DispatchPeer` (see below): a replaced or overflow-removed `Peer` gets `ErrStalePeer`, never reaching `Logic.Handle`.
- **A slow peer gets closed, not the room loop stalled.** Delivery goes through a bounded per-peer queue (`PeerQueue`); if it overflows, the Hub closes that one connection. The room and every other peer are unaffected. A failed `Peer.Send` (a broken connection, not just a full queue) is reported back to the room loop the same way and removes exactly that connection — never a peer that has since replaced it.
- **Outbound payloads are copied before queueing.** Delivery happens later, on a separate per-peer writer goroutine, so `Broadcast`/`SendUser`/`Dispatch`/`DispatchPeer` never retain a caller's own byte slice — mutating it right after a call returns is safe.
- **`ResetTimer` is generation-safe too.** Every call bumps that timer name's generation; the room loop discards an expiry whose generation is no longer current, so a timer callback that was blocked waiting for queue capacity can never fire after `Logic` already reset it.
- **There is no transactional rollback.** If `Handle` calls `Broadcast`/`SendUser`/etc. and then returns an error, those deliveries already happened. Validate before emitting, not emit-then-validate. This also applies to `Join` itself: an `EventJoin` handler error leaves the membership and snapshot delivery `Join` already applied in place — a transport adapter that gives up on a failed `Join` must still call `Leave` to clean that up, or a dead `Peer` stays registered indefinitely (see the WebSocket adapter section below).
- **Rooms are created and evicted implicitly.** A room springs into existence on a `Principal`'s first `Join` and is evicted once it has had zero live peers for longer than `ReconnectWindow` — eviction cancels pending timers and discards all in-memory state. A bot `Principal` (one that only ever calls `Dispatch`, never `Join`) never counts as a live peer and never keeps a room alive.

## Bots and transports: `Dispatch` vs `DispatchPeer`

A `Principal` can call `Hub.Dispatch` (as `EventAction`) against an already-existing room without ever calling `Join` — no `Peer`, no `Snapshot`, no membership entry. This is the bot/host-side path: unrestricted, membership-free, and deliberately *not* part of the `Coordinator` interface adapters depend on. `Dispatch` against a room that has never existed or has already been evicted returns `ErrRoomNotFound`.

A transport connection must instead use `Hub.DispatchPeer(ctx, event, peer)` — the method `Coordinator` actually exposes, and what `realtime/websocket.View` calls. It requires `peer` to be exactly the currently installed connection for `event.Principal` in that room; a stale, replaced, overflow-removed, or never-joined peer gets `ErrStalePeer` before `Logic.Handle` ever runs. This is what stops a connection that has already been superseded — by a reconnect, or by the Hub closing it after an outbound-queue overflow — from continuing to act as if it were still current.

tanGO does not add a `Principal.Bot`/`Kind` field: the Hub orders and delivers identically regardless of actor, so an app that needs to distinguish bot from human owns that convention itself.

## Shutting down

`Hub.Close(ctx)` is idempotent: it rejects further `Join`/`Dispatch`/`DispatchPeer` with `ErrClosed`, stops every room's pending timers, closes every live peer, and waits for every room loop to actually exit (or for `ctx` to be canceled, in which case it returns promptly with `ctx.Err()` while shutdown continues best-effort in the background). An operation already queued, or blocked trying to enqueue, at the moment `Close` began also resolves to `ErrClosed` — `ErrRoomNotFound` stays reserved for an actual room eviction, never Hub-wide shutdown.

`Hub.Close` integrates with `tango.ServeContext` by registering it as a `Lifecycle`'s `Stop`:

```go
registry.RegisterLifecycle(tango.Lifecycle{
    Name: "chat-hub",
    Stop: hub.Close,
})
```

then calling `tango.ServeContext` (not `tango.Serve`) with a caller-cancelable context, typically built with `signal.NotifyContext`. See [application lifecycle](application-lifecycle.md) and `examples/realtime-chat`, which wires this exact pattern.

## The WebSocket adapter

```go
func View(coordinator realtime.Coordinator, authenticate Authenticate, roomID func(*tango.Context) (string, error)) tango.View
```

Mount it as an ordinary route:

```go
view := realtimews.View(hub, authenticate, func(ctx *tango.Context) (string, error) {
    return ctx.Param("id"), nil
})
registry.Routes().Include("/", tango.URLs{
    tango.Path(http.MethodGet, "/rooms/{id}/ws", view),
})
```

`authenticate` (`func(*http.Request) (realtime.Principal, error)`) runs on the plain HTTP request before any upgrade is attempted — a failure responds with an ordinary JSON 401, no partially-established socket. It may call `auth/jwt.Service.Verify` directly (not `Service.Middleware`, which targets ordinary HTTP routes), extracting the token with `jwt.BearerToken` or, since a browser WebSocket handshake cannot set a custom header, `jwt.QueryToken`. It may just as easily use a cookie session or anything else — `realtime`/`realtime/websocket` never import `auth`, `auth/jwt`, or `accounts`.

`View` calls `coordinator.Join` and blocks until it returns successfully before starting the read loop — a client message sent immediately after the socket opens can never race ahead of the room loop's own membership-apply, which is exactly why `Hub.Join` is synchronous in the first place. Every inbound message is then dispatched through `coordinator.DispatchPeer`, carrying the same `Peer` `View` registered at `Join` — never the unrestricted `Hub.Dispatch` — so a connection the Hub has already replaced or closed for a queue overflow can never keep dispatching as if it were still current. Inbound messages are capped at `DefaultMaxMessageSize` (32 KiB); an oversized frame closes the connection rather than being buffered.

`Leave` is called exactly once, with a fresh bounded background context, in both places the connection can end: after the read loop returns normally, and after a failed `Join`. The latter exists because `Join` has no rollback (see above) — an `EventJoin` handler error can leave membership installed even though `View` is about to close the socket, so `View` always calls `Leave` before closing it. If `Join` failed before membership existed (e.g. a `Snapshot` error), that `Leave` is simply a safe, idempotent no-op.

## Deliberate limits

- Single-process, in-memory only — no distributed pub/sub, no cross-instance presence.
- No persistence or replay of room state across a restart or past a room's eviction.
- No automatic game rules, bot AI, or matchmaking — `Logic` is entirely host-written.
- No configurable backpressure policy beyond "close the slow peer."

See [limitations](../limitations.md) for the full list and the numeric defaults.

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

- **`Hub.Join`/`Leave`/`Dispatch` block the caller** until the room loop has actually applied the operation — not until any network I/O completes, just until room state reflects it. `Dispatch` returns `Logic.Handle`'s own error, so a caller (including a bot's own scheduler) can observe domain rejection, not merely "enqueued OK."
- **Peer replacement is generation-safe.** Joining with a new `Peer` for a `(RoomID, Principal)` that already has a live one replaces it; the old `Peer`'s eventual `Leave` — its read loop finally noticing the close — is recognized as stale and is a no-op, never removing the replacement. `Peer` implementations must be comparable (a pointer type) for this to work.
- **A slow peer gets closed, not the room loop stalled.** Delivery goes through a bounded per-peer queue (`PeerQueue`); if it overflows, the Hub closes that one connection. The room and every other peer are unaffected.
- **`ResetTimer` is generation-safe too.** Every call bumps that timer name's generation; the room loop discards an expiry whose generation is no longer current, so a timer callback that was blocked waiting for queue capacity can never fire after `Logic` already reset it.
- **There is no transactional rollback.** If `Handle` calls `Broadcast`/`SendUser`/etc. and then returns an error, those deliveries already happened. Validate before emitting, not emit-then-validate.
- **Rooms are created and evicted implicitly.** A room springs into existence on a `Principal`'s first `Join` and is evicted once it has had zero live peers for longer than `ReconnectWindow` — eviction cancels pending timers and discards all in-memory state. A bot `Principal` (one that only ever calls `Dispatch`, never `Join`) never counts as a live peer and never keeps a room alive.

## Bots

A `Principal` can call `Hub.Dispatch` (as `EventAction`) against an already-existing room without ever calling `Join` — no `Peer`, no `Snapshot`, no membership entry. `Dispatch` against a room that has never existed or has already been evicted returns `ErrRoomNotFound`. tanGO does not add a `Principal.Bot`/`Kind` field: the Hub orders and delivers identically regardless of actor, so an app that needs to distinguish bot from human owns that convention itself.

## Shutting down

`Hub.Close(ctx)` is idempotent: it rejects further `Join`/`Dispatch` with `ErrClosed`, stops every room's pending timers, closes every live peer, and waits for every room loop to actually exit (or for `ctx` to be canceled, in which case it returns promptly with `ctx.Err()` while shutdown continues best-effort in the background).

tanGO has no general application-lifecycle or graceful-shutdown mechanism today — `tango.Serve` is a bare `http.ListenAndServe` call, for every app, not just one using `realtime`. `Hub.Close` is therefore a plain method a host wires into its own shutdown path (an `http.Server` it manages itself, a signal handler, etc.) if it has one. There is no `tango.Serve`/`Config` integration in v0.1.

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

`View` calls `coordinator.Join` and blocks until it returns successfully before starting the read loop — a client message sent immediately after the socket opens can never race ahead of the room loop's own membership-apply, which is exactly why `Hub.Join` is synchronous in the first place. Inbound messages are capped at `DefaultMaxMessageSize` (32 KiB); an oversized frame closes the connection rather than being buffered. `Leave` is called exactly once, when the read loop ends for any reason.

## Deliberate limits

- Single-process, in-memory only — no distributed pub/sub, no cross-instance presence.
- No persistence or replay of room state across a restart or past a room's eviction.
- No automatic game rules, bot AI, or matchmaking — `Logic` is entirely host-written.
- No configurable backpressure policy beyond "close the slow peer."
- No `tango.Serve` shutdown integration; `Hub.Close` must be wired manually.

See [limitations](../limitations.md) for the full list and the numeric defaults.

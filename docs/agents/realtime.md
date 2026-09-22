# Agent Recipe: Realtime Rooms and WebSockets

Use this for a chat room, turn-based game, presence feature, or any other case where multiple connected actors need ordered, shared state.

Canonical example: `examples/realtime-chat`. Human guide: `docs/guides/realtime-websockets.md`. Architecture ADR: `docs/adr/0028-realtime-rooms-use-a-single-owner-event-loop-not-locks.md`.

## Build

- Construct one `realtime.Hub` per room *type* — a host running chat and a game constructs two Hubs, not one with per-room overrides.
- Implement `Logic.Handle`/`Snapshot`; only call `*realtime.RoomContext` methods from inside those two calls, never store the context.
- Use `BroadcastExcept` for join notices — `EventJoin` fires after membership is installed, so a plain `Broadcast` would notify the joiner about their own join.
- Validate an action fully before calling `Broadcast`/`SendUser`/`ResetTimer` — there is no rollback if `Handle` returns an error afterward.
- Mount `realtime/websocket.View(hub, authenticate, roomID)` as an ordinary route; write your own `Authenticate` (may call `auth/jwt.Service.Verify` directly, a cookie session, or anything else). `View` dispatches every inbound message through `Hub.DispatchPeer`, never `Hub.Dispatch` — that's what makes a replaced or overflow-removed connection unable to keep acting. If you write your own adapter instead of using `View`, always call `Leave` after a failed `Join` too — `Join` has no rollback, so an `EventJoin` handler error can leave membership installed with no connection left to remove it.
- Wire `Hub.Close(ctx)` into your own shutdown path if you have one — `tango.Serve` has no built-in graceful shutdown.

## Choose The Room Model

- One live `Peer` per `(RoomID, Principal)` always — a second `Join` replaces, never coexists with, an earlier one. `Peer` must be non-nil and comparable (a pointer type) — `Join` rejects anything else, including a typed-nil pointer inside a non-nil `Peer` interface, with `ErrInvalidPeer`.
- A `Principal` can `Dispatch` without ever `Join`ing — that's how a bot acts. It never appears in membership and never keeps an empty room alive. Only `Dispatch` (host/bot-facing) allows this; `DispatchPeer` (transport-facing) always requires current membership, or you get `ErrStalePeer`.
- Rooms are created on first `Join` and evicted after `ReconnectWindow` once empty — don't build your own room-lifecycle bookkeeping on top.

## Don't

- Do not call `Hub.Dispatch` from a transport adapter — use `DispatchPeer(ctx, event, peer)` instead, so a stale/replaced/overflow-removed connection is rejected before it ever reaches `Logic.Handle`.
- Do not treat `Payload []byte` as anything but opaque — `realtime` never parses it; pick your own encoding. It is safe to mutate your own buffer right after `Dispatch`/`DispatchPeer` returns — delivery always copies first.
- Do not add a `Principal.Bot`/`Kind` field — the Hub behaves identically regardless of actor; distinguish in your own `Logic` if you need to.
- Do not expect `Join`/`Leave`/`Dispatch`/`DispatchPeer` to return before the room loop has actually applied them (including running `Logic.Handle` for `Join`/`Leave`'s own `EventJoin`/`EventLeave`) — they are synchronous by design, not fire-and-forget.
- Do not assume a failed `Join` means nothing happened — membership and any snapshot delivery it already applied stay applied; call `Leave` to clean up (see `realtime/websocket.View`'s own pattern).
- Do not assume `Hub.Close` integrates with `tango.Serve`/`Config` — it does not, in v0.1.
- Do not rely on `realtime` for persistence, replay, or distributed/multi-process rooms — single-process, in-memory only.

## Check

- Exercise join, leave, a rejected action, an oversized WebSocket message, and a reconnect within `ReconnectWindow`.
- Verify a bot `Dispatch` against a nonexistent/evicted room returns `ErrRoomNotFound`, and that a replaced/overflow-removed `Peer`'s `DispatchPeer` returns `ErrStalePeer`.
- Verify an op queued or blocked when `Hub.Close` begins returns `ErrClosed`, not `ErrRoomNotFound`.
- Run `docs/agents/checklist.md` before stopping.

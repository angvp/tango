# Agent Recipe: Realtime Rooms and WebSockets

Use this for a chat room, turn-based game, presence feature, or any other case where multiple connected actors need ordered, shared state.

Canonical example: `examples/realtime-chat`. Human guide: `docs/guides/realtime-websockets.md`. Architecture ADR: `docs/adr/0028-realtime-rooms-use-a-single-owner-event-loop-not-locks.md`.

## Build

- Construct one `realtime.Hub` per room *type* — a host running chat and a game constructs two Hubs, not one with per-room overrides.
- Implement `Logic.Handle`/`Snapshot`; only call `*realtime.RoomContext` methods from inside those two calls, never store the context.
- Use `BroadcastExcept` for join notices — `EventJoin` fires after membership is installed, so a plain `Broadcast` would notify the joiner about their own join.
- Validate an action fully before calling `Broadcast`/`SendUser`/`ResetTimer` — there is no rollback if `Handle` returns an error afterward.
- Mount `realtime/websocket.View(hub, authenticate, roomID)` as an ordinary route; write your own `Authenticate` (may call `auth/jwt.Service.Verify` directly, a cookie session, or anything else).
- Wire `Hub.Close(ctx)` into your own shutdown path if you have one — `tango.Serve` has no built-in graceful shutdown.

## Choose The Room Model

- One live `Peer` per `(RoomID, Principal)` always — a second `Join` replaces, never coexists with, an earlier one.
- A `Principal` can `Dispatch` without ever `Join`ing — that's how a bot acts. It never appears in membership and never keeps an empty room alive.
- Rooms are created on first `Join` and evicted after `ReconnectWindow` once empty — don't build your own room-lifecycle bookkeeping on top.

## Don't

- Do not treat `Payload []byte` as anything but opaque — `realtime` never parses it; pick your own encoding.
- Do not add a `Principal.Bot`/`Kind` field — the Hub behaves identically regardless of actor; distinguish in your own `Logic` if you need to.
- Do not expect `Join`/`Leave`/`Dispatch` to return before the room loop has actually applied them — they are synchronous by design, not fire-and-forget.
- Do not assume `Hub.Close` integrates with `tango.Serve`/`Config` — it does not, in v0.1.
- Do not rely on `realtime` for persistence, replay, or distributed/multi-process rooms — single-process, in-memory only.

## Check

- Exercise join, leave, a rejected action, an oversized WebSocket message, and a reconnect within `ReconnectWindow`.
- Verify a bot `Dispatch` against a nonexistent/evicted room returns `ErrRoomNotFound`.
- Run `docs/agents/checklist.md` before stopping.

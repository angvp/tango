# Realtime chat example

This example shows a broadcast chat room built on tanGO's `realtime` and
`realtime/websocket` packages, authenticated with `auth/jwt` (Milestone 26).
It demonstrates:

- one `realtime.Hub` serving one room type (`chatLogic`), mounted at
  `GET /rooms/{id}/ws`;
- `realtime.RoomContext.Broadcast`/`BroadcastExcept` for chat messages and
  join/leave notices;
- `realtime.RoomContext.ResetTimer` for a per-user idle notice;
- `auth/jwt.QueryToken` for authentication, since a browser WebSocket
  handshake cannot set a custom `Authorization` header the way an ordinary
  HTTP client can — this is the concrete transport `QueryToken` exists for
  (see Milestone 26's `docs/limitations.md` note on query-token logging
  exposure);
- reconnecting within the room's reconnect window restores membership and
  delivers a fresh `Logic.Snapshot`.

Its hardcoded JWT secret and `-issue-token` flag are development-only
tools, not a production credential or authentication endpoint — same
convention as `examples/jwt-api`.

```sh
go run . -issue-token alice
go run .
# in another terminal, using any WebSocket client:
#   connect to ws://localhost:8000/rooms/demo/ws?token=<token>
```

Deliberately out of scope for this example (and for `realtime` itself in
v0.1): no persistence across a restart, no distributed/multi-process
rooms, and no chat history replay on join — `Logic.Snapshot` here is a
static welcome message, not a message backlog.

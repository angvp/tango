// Package websocket adapts realtime.Coordinator to an HTTP WebSocket
// upgrade, wrapping github.com/coder/websocket. No coder/websocket type
// appears in this package's own public API — see this milestone's ADR on
// wrapping third-party dependencies behind a translated contract.
package websocket

import (
	"context"
	"net/http"
	"time"

	ws "github.com/coder/websocket"

	"github.com/angvp/tango"
	"github.com/angvp/tango/realtime"
)

// DefaultMaxMessageSize bounds one inbound WebSocket message. It matches
// coder/websocket's own default, so View's behavior is explicit rather
// than an unstated inherited default.
const DefaultMaxMessageSize = 32 * 1024

// leaveTimeout bounds the best-effort Leave call View makes once a
// connection's read loop has ended — long enough for a healthy room to
// apply it, short enough not to leave a goroutine hanging indefinitely if
// something is badly wrong.
const leaveTimeout = 5 * time.Second

// Authenticate resolves the requesting Principal from a plain HTTP request,
// before any WebSocket upgrade is attempted. It may use auth/jwt (see
// jwt.Service.Verify, called directly — not via jwt.Service.Middleware,
// since that targets ordinary HTTP routes), a cookie session, or anything
// else; this package never imports auth, auth/jwt, or accounts.
type Authenticate func(*http.Request) (realtime.Principal, error)

// View upgrades an authenticated request to a WebSocket connection and
// runs it against coordinator until the connection ends. Mount it as an
// ordinary route, e.g. tango.Path("GET", "/rooms/{id}/ws", View(hub,
// authenticate, roomIDFromPath)).
//
// authenticate runs before the upgrade is attempted: a failure responds
// with a plain JSON 401 and never touches the WebSocket handshake.
// roomID is resolved the same way, before upgrading, so a bad room
// reference fails as an ordinary HTTP 400 rather than an upgraded
// connection that immediately closes.
//
// Join is called, and must return successfully, before View starts
// reading any client message — a message sent immediately after the
// socket opens must never race ahead of the room loop's own
// membership-apply. This is exactly why realtime.Hub.Join is synchronous.
func View(coordinator realtime.Coordinator, authenticate Authenticate, roomID func(*tango.Context) (string, error)) tango.View {
	return func(ctx *tango.Context) error {
		principal, err := authenticate(ctx.Request())
		if err != nil {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}

		room, err := roomID(ctx)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "bad request"})
		}

		conn, err := ws.Accept(ctx.ResponseWriter(), ctx.Request(), nil)
		if err != nil {
			// Accept has already written an HTTP error response on failure.
			return nil
		}
		defer conn.CloseNow()
		conn.SetReadLimit(DefaultMaxMessageSize)

		peer := &connPeer{conn: conn}
		if err := coordinator.Join(ctx.Context(), room, principal, peer); err != nil {
			conn.Close(ws.StatusInternalError, "join failed")
			return nil
		}

		readLoop(ctx.Context(), coordinator, room, principal, peer, conn)

		leaveCtx, cancel := context.WithTimeout(context.Background(), leaveTimeout)
		defer cancel()
		_ = coordinator.Leave(leaveCtx, room, principal, peer)
		return nil
	}
}

// readLoop reads client messages until the connection ends for any reason
// (client close, protocol error, context cancellation, or a server-side
// close from a Hub-side peer-queue overflow) and dispatches each as an
// EventAction. It returns exactly once, so its caller's single Leave call
// site is guaranteed to run exactly once per connection.
func readLoop(ctx context.Context, coordinator realtime.Coordinator, roomID string, principal realtime.Principal, peer *connPeer, conn *ws.Conn) {
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		_ = coordinator.Dispatch(ctx, realtime.Event{
			Kind:      realtime.EventAction,
			RoomID:    roomID,
			Principal: principal,
			Payload:   payload,
		})
	}
}

// connPeer implements realtime.Peer over one coder/websocket connection.
// It is a pointer type so realtime.Hub's identity-based stale-Leave
// detection works correctly.
type connPeer struct {
	conn *ws.Conn
}

func (p *connPeer) Send(ctx context.Context, payload []byte) error {
	return p.conn.Write(ctx, ws.MessageBinary, payload)
}

func (p *connPeer) Close() error {
	return p.conn.Close(ws.StatusNormalClosure, "")
}

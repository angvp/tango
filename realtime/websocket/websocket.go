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

// viewConfig holds View's tunables, built from the ViewOption values
// passed to it.
type viewConfig struct {
	acceptOptions *ws.AcceptOptions
}

// ViewOption configures View.
type ViewOption func(*viewConfig)

// WithOriginPatterns configures the host patterns coder/websocket's Accept
// treats as authorized origins for the upgrade's Origin header check —
// required whenever the connecting page's origin differs from this route's
// own Host (a separate frontend dev server on another port, a different
// subdomain in production, etc.). Without this, Accept applies its default
// same-origin check and responds 403 to any cross-origin handshake,
// regardless of whether authenticate would have accepted the request.
//
// Patterns follow ws.AcceptOptions.OriginPatterns's own syntax (a bare host,
// optionally with a scheme, "*" wildcards allowed within a segment). Do not
// pass "*" alone as a pattern — that authorizes any origin unconditionally;
// use WithInsecureSkipVerify instead so that intent is explicit at the call
// site rather than hidden behind a pattern that reads like a real host.
func WithOriginPatterns(patterns ...string) ViewOption {
	return func(c *viewConfig) {
		c.acceptOptions = ensureAcceptOptions(c.acceptOptions)
		c.acceptOptions.OriginPatterns = patterns
	}
}

// WithInsecureSkipVerify disables Accept's origin verification entirely —
// every origin is accepted. Prefer WithOriginPatterns; this exists for the
// same reason coder/websocket exposes InsecureSkipVerify directly: some
// deployments (e.g. serving to native clients, or behind infrastructure
// that already enforces origin) have no real origin to check.
func WithInsecureSkipVerify() ViewOption {
	return func(c *viewConfig) {
		c.acceptOptions = ensureAcceptOptions(c.acceptOptions)
		c.acceptOptions.InsecureSkipVerify = true
	}
}

func ensureAcceptOptions(opts *ws.AcceptOptions) *ws.AcceptOptions {
	if opts == nil {
		return &ws.AcceptOptions{}
	}
	return opts
}

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
//
// By default, the upgrade is rejected with 403 for any request whose
// Origin header doesn't match this route's own Host — coder/websocket's
// own same-origin protection. Pass WithOriginPatterns (or, if there's
// truly no origin to check, WithInsecureSkipVerify) to allow a connecting
// page served from a different origin, such as a frontend dev server on
// another port or a separate production domain.
func View(coordinator realtime.Coordinator, authenticate Authenticate, roomID func(*tango.Context) (string, error), opts ...ViewOption) tango.View {
	config := viewConfig{}
	for _, opt := range opts {
		opt(&config)
	}

	return func(ctx *tango.Context) error {
		principal, err := authenticate(ctx.Request())
		if err != nil {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}

		room, err := roomID(ctx)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "bad request"})
		}

		conn, err := ws.Accept(ctx.ResponseWriter(), ctx.Request(), config.acceptOptions)
		if err != nil {
			// Accept has already written an HTTP error response on failure.
			return nil
		}
		defer conn.CloseNow()
		conn.SetReadLimit(DefaultMaxMessageSize)

		peer := &connPeer{conn: conn}
		if err := coordinator.Join(ctx.Context(), room, principal, peer); err != nil {
			// Join has no rollback: an EventJoin handler error is returned
			// while membership it already installed stays installed (see
			// realtime.Hub.Join's doc comment). A Join failure that occurs
			// before membership exists — e.g. a Snapshot error — makes this
			// Leave a safe, idempotent no-op; one that occurs after is what
			// actually needs this call, or the dead Peer stays registered
			// indefinitely and the room can never evict.
			bestEffortLeave(coordinator, room, principal, peer)
			conn.Close(ws.StatusInternalError, "join failed")
			return nil
		}

		readLoop(ctx.Context(), coordinator, room, principal, peer, conn)

		bestEffortLeave(coordinator, room, principal, peer)
		return nil
	}
}

// bestEffortLeave calls Leave with a fresh, bounded background context —
// shared by both places View must clean up membership: after the read loop
// ends normally, and after a failed Join that may have installed membership
// anyway. Leave is always a safe idempotent no-op if there was nothing to
// remove.
func bestEffortLeave(coordinator realtime.Coordinator, roomID string, principal realtime.Principal, peer realtime.Peer) {
	leaveCtx, cancel := context.WithTimeout(context.Background(), leaveTimeout)
	defer cancel()
	_ = coordinator.Leave(leaveCtx, roomID, principal, peer)
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
		_ = coordinator.DispatchPeer(ctx, realtime.Event{
			Kind:      realtime.EventAction,
			RoomID:    roomID,
			Principal: principal,
			Payload:   payload,
		}, peer)
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

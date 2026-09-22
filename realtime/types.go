// Package realtime provides transport-neutral, single-process room
// coordination. Each active room is owned by exactly one goroutine (its
// room loop), which is the sole mutator of that room's membership, timers,
// and every call into host-supplied Logic — see the package's design ADR
// for why (no distributed pub/sub, no locking-based shared state).
package realtime

import (
	"context"
	"errors"
	"time"
)

// Principal is the stable, transport-independent actor identity a host
// supplies to Join/Dispatch. It is deliberately just an opaque UserID:
// realtime does not distinguish a human actor from a bot at this layer —
// an application needing that distinction owns its own convention.
type Principal struct {
	UserID string
}

// EventKind discriminates the shapes of activity a room's event loop
// processes. It carries no application meaning beyond routing.
type EventKind uint8

const (
	EventAction EventKind = iota
	EventTimer
	EventJoin
	EventLeave
)

// Event is one unit of room activity. Payload is opaque to realtime — it
// is never parsed or inspected by the framework, only by Logic.
type Event struct {
	Kind      EventKind
	RoomID    string
	Principal Principal
	Timer     string // populated only for EventTimer
	Payload   []byte
}

// Peer is one live, replaceable transport connection for one Principal in
// one room. Implementations must be comparable (a pointer-receiver type is
// the natural choice) so the Hub can recognize a stale Leave from an
// already-replaced connection by identity.
type Peer interface {
	Send(context.Context, []byte) error
	Close() error
}

// RoomContext is the handle Logic uses to affect its own room. Its methods
// are only ever called from that room's own event-loop goroutine, while
// handling one Event or computing one Snapshot — never concurrently, and
// never valid to retain past that call.
type RoomContext struct {
	room *room
}

// Broadcast delivers payload to every currently live peer in the room.
func (r *RoomContext) Broadcast(payload []byte) error {
	r.room.broadcast(payload, "")
	return nil
}

// BroadcastExcept delivers payload to every live peer except userID.
func (r *RoomContext) BroadcastExcept(userID string, payload []byte) error {
	r.room.broadcast(payload, userID)
	return nil
}

// SendUser delivers payload to userID's live peer in this room, if any. It
// is a no-op if userID has no live peer here.
func (r *RoomContext) SendUser(userID string, payload []byte) error {
	r.room.sendUser(userID, payload)
	return nil
}

// Logic is the host-supplied domain behavior for one room.
type Logic interface {
	// Handle processes one Event. Deliveries made via RoomContext before
	// returning a non-nil error are not rolled back — validate before
	// emitting, not emit-then-validate.
	Handle(*RoomContext, Event) error
	// Snapshot returns the current-state payload a Principal should
	// receive immediately upon joining (or rejoining) the room.
	Snapshot(*RoomContext, Principal) ([]byte, error)
}

// Factory constructs the Logic for one room ID. One Hub always uses the
// same Factory, so one Hub corresponds to one room type; a host running
// several kinds of room constructs several Hubs.
type Factory func(roomID string) Logic

// Options configures a Hub. A zero field is replaced with this package's
// corresponding Default* constant; a negative field is a construction
// error.
type Options struct {
	// ReconnectWindow is how long an empty room's in-memory state is
	// retained before implicit eviction.
	ReconnectWindow time.Duration
	// PeerQueue is the bounded outbound-delivery queue depth per peer.
	PeerQueue int
	// RoomQueue is the bounded inbound event-queue depth per room.
	RoomQueue int
}

const (
	DefaultReconnectWindow = 30 * time.Second
	DefaultPeerQueue       = 16
	DefaultRoomQueue       = 64
)

// Coordinator is the surface realtime/websocket (and any other adapter)
// depends on. It is kept intentionally narrow: its purpose is adapter
// testability against a fake implementation, not general swappability.
type Coordinator interface {
	Join(context.Context, string, Principal, Peer) error
	Leave(context.Context, string, Principal, Peer) error
	Dispatch(context.Context, Event) error
}

var (
	// ErrRoomNotFound is returned by Dispatch against a room that has
	// never existed or has already been evicted.
	ErrRoomNotFound = errors.New("tango realtime: room not found")
	// ErrClosed is returned by Join/Dispatch once Hub.Close has begun.
	ErrClosed = errors.New("tango realtime: hub is closed")
)

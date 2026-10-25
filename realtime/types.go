// Package realtime provides transport-neutral, single-process room
// coordination. Each active room is owned by exactly one goroutine (its
// room loop), which is the sole mutator of that room's membership, timers,
// and every call into host-supplied Logic. There is no distributed pub/sub and no
// locking-based shared state.
package realtime

import (
	"context"
	"errors"
	"time"

	"github.com/angvp/tango/observability"
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
// the natural choice) so the Hub can recognize a stale Leave or DispatchPeer
// call from an already-replaced connection by identity. Join validates this
// at the boundary — a nil Peer, or one whose dynamic type is not comparable,
// is rejected with ErrInvalidPeer rather than ever being admitted into
// membership, so no later comparison against it can panic.
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

// Framer is an optional interface a Logic implements to opt its room in to
// sequenced framing. Frame runs on the room goroutine, like every Logic
// callback, and a panic in Frame is not recovered: it crashes the process,
// as a panic in Handle or Snapshot does.
//
// payload is the callee's own per-member copy: Frame may inspect or modify
// it without affecting other members or the caller. The returned slice
// becomes the queued buffer and is owned by the room from the moment Frame
// returns; the implementation must not retain or modify it afterwards. A nil
// or empty result sends nothing to that member. A non-nil error closes that
// member exactly as queue overflow does.
//
// On a connection whose Logic implements Framer, every payload admitted to
// the member's queue is passed through Frame with a sequence that starts at
// 1 for the join snapshot and increases by 1 for each later admitted payload,
// in queue order, with no gaps. The sequence is per member (per connection;
// reconnecting restarts at 1), not per room, and records admission to the
// outgoing queue, not delivery or acknowledgement by the peer. A payload that
// is not admitted (excluded or unknown member, empty encoder result, encoder
// error, queue overflow) uses no sequence; a socket write failure ends the
// member.
type Framer interface {
	Frame(to Principal, seq uint64, payload []byte) ([]byte, error)
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
	// Recorder receives bounded Hub operation metrics. Implementations must
	// be safe for concurrent use. Nil uses observability.NopRecorder{}.
	Recorder observability.Recorder
}

const (
	DefaultReconnectWindow = 30 * time.Second
	DefaultPeerQueue       = 16
	DefaultRoomQueue       = 64
)

// Coordinator is the surface realtime/websocket (and any other adapter)
// depends on. It is kept intentionally narrow: its purpose is adapter
// testability against a fake implementation, not general swappability.
// It deliberately excludes Hub.Dispatch — a transport must always act
// through DispatchPeer, which is bound to its specific live connection, not
// the unrestricted membership-free path bots and other trusted host-side
// producers use.
type Coordinator interface {
	Join(context.Context, string, Principal, Peer) error
	Leave(context.Context, string, Principal, Peer) error
	DispatchPeer(context.Context, Event, Peer) error
}

var (
	// ErrRoomNotFound is returned against a room that has never existed or
	// has already been evicted.
	ErrRoomNotFound = errors.New("tango realtime: room not found")
	// ErrClosed is returned by Join/Dispatch/DispatchPeer once Hub.Close
	// has begun — including for operations that were already queued, or
	// blocked trying to enqueue, at the moment it began.
	ErrClosed = errors.New("tango realtime: hub is closed")
	// ErrStalePeer is returned by DispatchPeer when peer is not exactly
	// the currently installed connection for the event's Principal in that
	// room — stale, replaced, overflow-removed, or never a member.
	ErrStalePeer = errors.New("tango realtime: peer is not the current connection")
	// ErrInvalidPeer is returned by Join for a nil Peer, or one whose
	// dynamic type is not comparable.
	ErrInvalidPeer = errors.New("tango realtime: peer must be non-nil and comparable")
)

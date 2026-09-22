package realtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Hub owns every active room for one Factory. Rooms are not pre-declared:
// they are created implicitly on a Principal's first Join and evicted
// implicitly once empty past Options.ReconnectWindow.
type Hub struct {
	factory Factory
	opts    Options

	mu     sync.Mutex
	rooms  map[string]*room
	closed bool

	closeCh   chan struct{}
	closeOnce sync.Once
	allDone   chan struct{}

	// newTimer is the injectable timer source behind eviction (and, later,
	// named-timer) scheduling — swapped out by tests for a deterministic
	// fake so no test depends on real wall-clock sleeps.
	newTimer func(time.Duration, func()) func() bool
}

// NewHub constructs a Hub. A zero Options field is replaced with this
// package's Default* constant; a negative field is rejected here, at
// construction, rather than surfacing later as a confusing runtime failure.
func NewHub(factory Factory, opts Options) (*Hub, error) {
	if factory == nil {
		return nil, fmt.Errorf("tango realtime: factory must not be nil")
	}
	if opts.ReconnectWindow < 0 {
		return nil, fmt.Errorf("tango realtime: reconnect window must not be negative")
	}
	if opts.ReconnectWindow == 0 {
		opts.ReconnectWindow = DefaultReconnectWindow
	}
	if opts.RoomQueue < 0 {
		return nil, fmt.Errorf("tango realtime: room queue depth must not be negative")
	}
	if opts.RoomQueue == 0 {
		opts.RoomQueue = DefaultRoomQueue
	}
	if opts.PeerQueue < 0 {
		return nil, fmt.Errorf("tango realtime: peer queue depth must not be negative")
	}
	if opts.PeerQueue == 0 {
		opts.PeerQueue = DefaultPeerQueue
	}

	return &Hub{
		factory:  factory,
		opts:     opts,
		rooms:    make(map[string]*room),
		closeCh:  make(chan struct{}),
		allDone:  make(chan struct{}),
		newTimer: defaultNewTimer,
	}, nil
}

func defaultNewTimer(d time.Duration, f func()) func() bool {
	t := time.AfterFunc(d, f)
	return t.Stop
}

// Join installs peer as principal's live connection in roomID, creating
// the room on first join. It blocks until the room's own event loop has
// applied the join (installed membership and enqueued Logic.Snapshot for
// delivery) or ctx is done.
func (h *Hub) Join(ctx context.Context, roomID string, principal Principal, peer Peer) error {
	if h.isClosed() {
		return ErrClosed
	}
	for {
		r := h.getOrCreateRoom(roomID)
		err := r.submit(ctx, roomOp{kind: opJoin, principal: principal, peer: peer})
		if errors.Is(err, ErrRoomNotFound) {
			// r was already mid-eviction when we grabbed it; retry against
			// a fresh room now that it has (or will have) removed itself.
			continue
		}
		return err
	}
}

// Leave removes peer as principal's live connection in roomID, if it is
// still the current one. A Leave for a room that no longer exists, or from
// a peer already superseded by a later Join, is a no-op — never an error.
func (h *Hub) Leave(ctx context.Context, roomID string, principal Principal, peer Peer) error {
	r := h.getRoom(roomID)
	if r == nil {
		return nil
	}
	err := r.submit(ctx, roomOp{kind: opLeave, principal: principal, peer: peer})
	if errors.Is(err, ErrRoomNotFound) {
		return nil
	}
	return err
}

// Dispatch delivers ev.Payload into ev.RoomID's Logic.Handle. ev.Kind must
// be EventAction — EventJoin/EventLeave/EventTimer are synthesized
// internally by the room loop itself, never accepted from a caller.
// Dispatch does not require the caller to have joined; this is how a bot
// Principal acts without ever holding a Peer.
func (h *Hub) Dispatch(ctx context.Context, ev Event) error {
	if ev.Kind != EventAction {
		return fmt.Errorf("tango realtime: Dispatch only accepts EventAction")
	}
	if h.isClosed() {
		return ErrClosed
	}
	r := h.getRoom(ev.RoomID)
	if r == nil {
		return ErrRoomNotFound
	}
	return r.submit(ctx, roomOp{kind: opDispatch, event: ev})
}

// SendUser delivers payload to userID's live peer in every room the Hub
// currently tracks where that user has one — a no-op in any room where
// they don't.
func (h *Hub) SendUser(ctx context.Context, userID string, payload []byte) error {
	h.mu.Lock()
	rooms := make([]*room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.Unlock()

	for _, r := range rooms {
		err := r.submit(ctx, roomOp{kind: opSendUser, principal: Principal{UserID: userID}, payload: payload})
		if err != nil && !errors.Is(err, ErrRoomNotFound) {
			return err
		}
	}
	return nil
}

// Close is idempotent: the first call rejects further Join/Dispatch with
// ErrClosed, stops every room's pending timers, closes every live peer,
// and waits for every room loop to actually exit. Every call (the first
// and any subsequent one) blocks until that has happened or ctx is done —
// a canceled ctx returns promptly with ctx.Err() while shutdown continues
// best-effort in the background; it is not a guarantee rooms have finished.
func (h *Hub) Close(ctx context.Context) error {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		rooms := make([]*room, 0, len(h.rooms))
		for _, r := range h.rooms {
			rooms = append(rooms, r)
		}
		h.mu.Unlock()

		close(h.closeCh)
		go func() {
			for _, r := range rooms {
				<-r.done
			}
			close(h.allDone)
		}()
	})

	select {
	case <-h.allDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) isClosed() bool {
	select {
	case <-h.closeCh:
		return true
	default:
		return false
	}
}

func (h *Hub) getOrCreateRoom(id string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[id]; ok {
		return r
	}
	r := newRoom(h, id)
	h.rooms[id] = r
	return r
}

func (h *Hub) getRoom(id string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[id]
}

func (h *Hub) removeRoom(id string, self *room) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[id] == self {
		delete(h.rooms, id)
	}
}

var _ Coordinator = (*Hub)(nil)

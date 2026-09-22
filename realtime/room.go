package realtime

import (
	"context"
	"sync"
)

type opKind uint8

const (
	opJoin opKind = iota
	opLeave
	opDispatch
	opSendUser
	opEvictCheck
)

// roomOp is one unit of work submitted to a room's inbox. reply is nil for
// internally-produced ops (eviction checks, and later timer expiry), which
// have no caller waiting on a result.
type roomOp struct {
	kind      opKind
	principal Principal
	peer      Peer
	event     Event
	payload   []byte
	evictGen  uint64
	reply     chan error
}

type member struct {
	peer   Peer
	queue  chan []byte
	cancel context.CancelFunc
}

// room is the single-owner event loop for one room ID: every membership
// change, delivery, and call into Logic happens on this goroutine alone.
// See the package doc and this milestone's ADR for why.
type room struct {
	id    string
	hub   *Hub
	logic Logic

	inbox chan roomOp
	done  chan struct{}

	// stopMu/stopped/inflight close the race between an external submit()
	// deciding to send into inbox and the room loop deciding to stop
	// draining it forever — see beginShutdown.
	stopMu   sync.Mutex
	stopped  bool
	inflight sync.WaitGroup

	members   map[string]*member
	evictGen  uint64
	evictStop func() bool
	closing   bool
}

func newRoom(hub *Hub, id string) *room {
	r := &room{
		id:      id,
		hub:     hub,
		logic:   hub.factory(id),
		inbox:   make(chan roomOp, hub.opts.RoomQueue),
		done:    make(chan struct{}),
		members: make(map[string]*member),
	}
	go r.run()
	return r
}

// submit enqueues op and blocks until the room loop has processed it (or
// ctx is done, the Hub is closing, or the room has already stopped). Used
// for every externally-triggered Join/Leave/Dispatch/SendUser.
//
// The stopMu/inflight dance closes an otherwise-real deadlock: without it,
// a submit() that reaches its channel send in the exact instant the room
// loop decides to stop draining inbox forever could enqueue an op nobody
// will ever read, hanging the caller. Registering as in-flight before
// attempting the send, under the same mutex beginShutdown uses to flip
// stopped, guarantees beginShutdown's inflight.Wait() cannot return until
// every such race has resolved one way or the other — after which a final
// drain (drainInbox) catches anything that did get enqueued. inflight only
// covers the enqueue attempt itself, never the subsequent reply-wait below
// — once an op is in the inbox, either the room loop's normal processing
// or drainInbox will resolve it, and inflight.Wait() must not block on
// that (drainInbox runs strictly after inflight.Wait() returns).
func (r *room) submit(ctx context.Context, op roomOp) error {
	r.stopMu.Lock()
	if r.stopped {
		r.stopMu.Unlock()
		return ErrRoomNotFound
	}
	r.inflight.Add(1)
	r.stopMu.Unlock()

	op.reply = make(chan error, 1)
	var sendErr error
	select {
	case r.inbox <- op:
	case <-ctx.Done():
		sendErr = ctx.Err()
	case <-r.hub.closeCh:
		sendErr = ErrClosed
	}
	r.inflight.Done()
	if sendErr != nil {
		return sendErr
	}

	select {
	case err := <-op.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// submitInternal enqueues op from an internal producer (eviction check,
// and later timer expiry) against the room's own lifecycle, not an
// external caller's context. It blocks for room-queue capacity and is
// silently discarded — lifecycle cancellation, not ordinary dropping — if
// the room closes before the enqueue can complete.
func (r *room) submitInternal(op roomOp) {
	select {
	case r.inbox <- op:
	case <-r.done:
	case <-r.hub.closeCh:
	}
}

func (r *room) run() {
	defer close(r.done)
	for {
		select {
		case op := <-r.inbox:
			r.process(op)
			if r.closing {
				r.shutdown()
				return
			}
		case <-r.hub.closeCh:
			r.shutdown()
			return
		}
	}
}

// beginShutdown makes the room stop accepting new submissions and waits
// for every submit() call already past the stopped-check to finish
// resolving (either its send into inbox landed, or it bailed via ctx/
// closeCh) — see submit's doc comment for why this is required, not just
// a nicety.
func (r *room) beginShutdown() {
	r.stopMu.Lock()
	r.stopped = true
	r.stopMu.Unlock()
	r.inflight.Wait()
}

// drainInbox replies ErrRoomNotFound to every op left in the queue once
// nothing can enqueue any more (call only after beginShutdown).
func (r *room) drainInbox() {
	for {
		select {
		case op := <-r.inbox:
			if op.reply != nil {
				op.reply <- ErrRoomNotFound
			}
		default:
			return
		}
	}
}

func (r *room) process(op roomOp) {
	switch op.kind {
	case opJoin:
		r.handleJoin(op)
	case opLeave:
		r.handleLeave(op)
	case opDispatch:
		r.handleDispatch(op)
	case opSendUser:
		r.handleSendUser(op)
	case opEvictCheck:
		r.handleEvictCheck(op)
	}
}

func (r *room) handleJoin(op roomOp) {
	rc := &RoomContext{room: r}
	snapshot, err := r.logic.Snapshot(rc, op.principal)
	if err != nil {
		op.reply <- err
		return
	}

	userID := op.principal.UserID
	if existing, ok := r.members[userID]; ok {
		r.closeMember(userID, existing)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &member{peer: op.peer, queue: make(chan []byte, r.hub.opts.PeerQueue), cancel: cancel}
	r.members[userID] = m
	go runPeerWriter(ctx, m)
	r.cancelEviction()

	r.deliver(userID, m, snapshot)
	op.reply <- nil

	_ = r.logic.Handle(rc, Event{Kind: EventJoin, RoomID: r.id, Principal: op.principal})
}

func (r *room) handleLeave(op roomOp) {
	userID := op.principal.UserID
	existing, ok := r.members[userID]
	if !ok || existing.peer != op.peer {
		// Stale (already-replaced) or unknown leave: idempotent no-op.
		op.reply <- nil
		return
	}
	r.closeMember(userID, existing)
	op.reply <- nil

	rc := &RoomContext{room: r}
	_ = r.logic.Handle(rc, Event{Kind: EventLeave, RoomID: r.id, Principal: op.principal})
}

func (r *room) handleDispatch(op roomOp) {
	rc := &RoomContext{room: r}
	op.reply <- r.logic.Handle(rc, op.event)
}

func (r *room) handleSendUser(op roomOp) {
	r.sendUser(op.principal.UserID, op.payload)
	op.reply <- nil
}

func (r *room) handleEvictCheck(op roomOp) {
	if op.evictGen != r.evictGen {
		return // superseded by a later join, leave, or reschedule
	}
	if len(r.members) != 0 {
		return
	}
	r.closing = true
}

func (r *room) shutdown() {
	r.hub.removeRoom(r.id, r)
	r.beginShutdown()
	r.drainInbox()
	r.cancelEviction()
	for userID, m := range r.members {
		delete(r.members, userID)
		m.cancel()
		go func(p Peer) { _ = p.Close() }(m.peer)
	}
}

func (r *room) broadcast(payload []byte, exceptUserID string) {
	for userID, m := range r.members {
		if userID == exceptUserID {
			continue
		}
		r.deliver(userID, m, payload)
	}
}

func (r *room) sendUser(userID string, payload []byte) {
	if m, ok := r.members[userID]; ok {
		r.deliver(userID, m, payload)
	}
}

// deliver enqueues payload for m without ever blocking the room loop: a
// full outbound queue means m is slow, so m is closed instead — the room
// and every other peer are unaffected.
func (r *room) deliver(userID string, m *member, payload []byte) {
	select {
	case m.queue <- payload:
	default:
		r.closeMember(userID, m)
	}
}

// closeMember removes m from membership (if it is still the current one)
// and closes its connection asynchronously, never blocking the room loop.
func (r *room) closeMember(userID string, m *member) {
	if current, ok := r.members[userID]; !ok || current != m {
		return
	}
	delete(r.members, userID)
	m.cancel()
	go func() { _ = m.peer.Close() }()
	if len(r.members) == 0 {
		r.scheduleEviction()
	}
}

func (r *room) scheduleEviction() {
	r.evictGen++
	gen := r.evictGen
	r.evictStop = r.hub.newTimer(r.hub.opts.ReconnectWindow, func() {
		r.submitInternal(roomOp{kind: opEvictCheck, evictGen: gen})
	})
}

func (r *room) cancelEviction() {
	if r.evictStop != nil {
		r.evictStop()
		r.evictStop = nil
	}
	r.evictGen++ // invalidate any fire already in flight past Stop's race window
}

func runPeerWriter(ctx context.Context, m *member) {
	for {
		select {
		case payload := <-m.queue:
			_ = m.peer.Send(ctx, payload)
		case <-ctx.Done():
			return
		}
	}
}

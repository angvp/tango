package realtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBlockedAndQueuedDispatchesReturnErrClosedOnHubClose covers finding
// 5's ErrClosed half: an operation blocked trying to enqueue, and one
// already queued but not yet processed, at the moment Hub.Close begins,
// must both resolve to ErrClosed — never ErrRoomNotFound, which is
// reserved for actual room eviction.
func TestBlockedAndQueuedDispatchesReturnErrClosedOnHubClose(t *testing.T) {
	reachedHandle := make(chan struct{})
	release := make(chan struct{})
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			select {
			case <-reachedHandle:
			default:
				close(reachedHandle)
			}
			<-release
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{RoomQueue: 1})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	r := hub.getRoom("room-1")

	// A occupies the room loop, blocked inside Handle.
	aDone := make(chan error, 1)
	go func() {
		aDone <- hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "room-1"})
	}()
	<-reachedHandle // inbox is now empty (A was already dequeued) with one free slot

	// B is placed directly into the inbox (bypassing submit, so its
	// presence there is certain rather than raced against a second
	// goroutine): already queued but not processed.
	bReply := make(chan error, 1)
	r.inbox <- roomOp{kind: opDispatch, event: Event{Kind: EventAction, RoomID: "room-1"}, reply: bReply}

	// C now genuinely blocks trying to send into the full (capacity-1,
	// already-occupied-by-B) inbox.
	cDone := make(chan error, 1)
	go func() {
		cDone <- hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "room-1"})
	}()

	closeDone := make(chan error, 1)
	go func() { closeDone <- hub.Close(context.Background()) }()

	// C is blocked purely on the inbox-full/closeCh select inside its own
	// submit() call, independent of the room loop — it resolves via
	// closeCh as soon as Close begins, with no need for A to finish.
	select {
	case err := <-cDone:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("blocked dispatch (C) error = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked dispatch (C) never returned")
	}

	// B's reply, by contrast, can only be produced by the room loop itself
	// (via drainInbox), which cannot run until it's done processing A — so
	// A must be released first.
	close(release)

	select {
	case err := <-bReply:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("queued-but-unprocessed dispatch (B) error = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued dispatch (B) reply never arrived")
	}

	select {
	case <-aDone:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight dispatch (A) never returned")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close never completed")
	}
}

// TestSubmitAgainstNormallyEvictedRoomReturnsErrRoomNotFound covers
// finding 5's other half: ErrRoomNotFound stays reserved for an ordinary
// eviction, not Hub-wide shutdown.
func TestSubmitAgainstNormallyEvictedRoomReturnsErrRoomNotFound(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, sched := newTestHub(t, factory, Options{})
	ctx := context.Background()

	peer := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, peer); err != nil {
		t.Fatalf("join: %v", err)
	}
	r := hub.getRoom("room-1")
	if err := hub.Leave(ctx, "room-1", Principal{UserID: "a"}, peer); err != nil {
		t.Fatalf("leave: %v", err)
	}
	sched.fireAll() // fires the eviction check; room is empty, so it evicts
	waitFor(t, func() bool {
		select {
		case <-r.done:
			return true
		default:
			return false
		}
	}, "room never finished its normal eviction shutdown")

	err := r.submit(ctx, roomOp{kind: opDispatch, event: Event{Kind: EventAction, RoomID: "room-1"}})
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("submit against a normally evicted room = %v, want ErrRoomNotFound", err)
	}
	if errors.Is(err, ErrClosed) {
		t.Fatalf("a normal eviction must never be reported as ErrClosed, got %v", err)
	}
}

// TestPayloadBytesAreCopiedBeforeAsyncDelivery covers the additional
// hardening item: outbound delivery is asynchronous (a separate writer
// goroutine drains each peer's queue), so a caller mutating its own buffer
// immediately after Dispatch returns must never be observed by the Peer.
//
// The Peer's writer is blocked (via fakePeer.block) before Dispatch even
// runs, so the second message Dispatch enqueues cannot possibly have been
// read out of the queue — let alone Sent — by the time the buffer is
// mutated. Without that block, an unblocked fakePeer could race ahead and
// consume the payload before the mutation happens, letting the test pass
// even if deliver() didn't copy anything.
func TestPayloadBytesAreCopiedBeforeAsyncDelivery(t *testing.T) {
	logic := &recordingLogic{}
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			// Broadcasts the caller's own slice directly, exactly the
			// pattern that would leak a later caller-side mutation if
			// delivery didn't copy it first.
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	peer := &fakePeer{block: make(chan struct{})}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, peer); err != nil {
		t.Fatalf("join: %v", err)
	}
	// The snapshot delivery is now stuck in the writer goroutine's Send
	// call, blocked on peer.block — nothing has been (or can be) read from
	// peer's outbound queue yet.

	buf := []byte("original")
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: buf}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Mutate the caller's own buffer while the writer is still blocked on
	// the *first* message — the second message (this Dispatch's) cannot
	// have been touched by anything yet except deliver()'s own copy.
	copy(buf, "mutated!")

	close(peer.block)

	got := waitForMessages(t, peer, 2)
	if string(got[1]) != "original" {
		t.Fatalf("peer received %q, want the unmutated original %q", got[1], "original")
	}
}

// TestReceivedOpAfterCloseChIsRejectedWithoutRunningLogic deterministically
// exercises the shutdown-select race itself: room.run's main select can
// pick its inbox case even though closeCh has also just become ready (Go's
// select makes no ordering promise between simultaneously ready cases), so
// handleReceivedOp re-checks closeCh after the dequeue and before
// r.process(op). Looping and hoping to observe Go's random tie-break choose
// the inbox case would be probabilistic; instead this constructs a room
// without starting its run() goroutine, so the exact race window —
// closeCh already closed at the moment an op is about to be processed —
// is reproduced on every run, not just sometimes.
func TestReceivedOpAfterCloseChIsRejectedWithoutRunningLogic(t *testing.T) {
	handled := make(chan Event, 1)
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		handled <- ev
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	r := &room{
		id:      "room-1",
		hub:     hub,
		logic:   logic,
		inbox:   make(chan roomOp, 4),
		done:    make(chan struct{}),
		members: make(map[string]*member),
		timers:  make(map[string]*roomTimer),
	}
	hub.mu.Lock()
	hub.rooms[r.id] = r
	hub.mu.Unlock()

	// A second op left sitting in inbox, unrelated to the one passed
	// directly to handleReceivedOp below — proves the shutdown this
	// triggers drains every other queued op with ErrClosed too, via the
	// normal drainInbox path, not just the one op handed to it directly.
	otherReply := make(chan error, 1)
	r.inbox <- roomOp{kind: opDispatch, event: Event{Kind: EventAction, RoomID: "room-1"}, reply: otherReply}

	// closeCh is already closed by the time this op is "received" —
	// exactly the race window the fix closes.
	close(hub.closeCh)

	op := roomOp{kind: opDispatch, event: Event{Kind: EventAction, RoomID: "room-1"}, reply: make(chan error, 1)}
	if stop := r.handleReceivedOp(op); !stop {
		t.Fatal("handleReceivedOp must report stop=true once closeCh is already closed")
	}

	select {
	case err := <-op.reply:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("op caught by the shutdown race = %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("op never got a reply")
	}

	select {
	case err := <-otherReply:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("other queued op = %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("other queued op never got a reply")
	}

	select {
	case ev := <-handled:
		t.Fatalf("Logic.Handle must never run for an op caught by the shutdown race, got %+v", ev)
	default:
	}
}

// TestReceivedOpNormalEvictionStillReturnsErrRoomNotFound is
// handleReceivedOp's other exit path, proven with the same deterministic
// construction: when the room decides to evict on its own (not because
// Hub.Close began), the reason handed to shutdown/drainInbox must stay
// ErrRoomNotFound, never ErrClosed.
func TestReceivedOpNormalEvictionStillReturnsErrRoomNotFound(t *testing.T) {
	logic := &recordingLogic{}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	r := &room{
		id:      "room-1",
		hub:     hub,
		logic:   logic,
		inbox:   make(chan roomOp, 4),
		done:    make(chan struct{}),
		members: make(map[string]*member), // empty, so handleEvictCheck decides to close
		timers:  make(map[string]*roomTimer),
	}
	hub.mu.Lock()
	hub.rooms[r.id] = r
	hub.mu.Unlock()

	otherReply := make(chan error, 1)
	r.inbox <- roomOp{kind: opDispatch, event: Event{Kind: EventAction, RoomID: "room-1"}, reply: otherReply}

	// hub.closeCh is deliberately left open: this is an ordinary eviction.
	op := roomOp{kind: opEvictCheck, evictGen: 0}
	if stop := r.handleReceivedOp(op); !stop {
		t.Fatal("handleReceivedOp must report stop=true once the room decides to evict")
	}

	select {
	case err := <-otherReply:
		if !errors.Is(err, ErrRoomNotFound) {
			t.Fatalf("queued op during a normal eviction = %v, want ErrRoomNotFound", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued op never got a reply")
	}
}

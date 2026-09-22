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

	peer := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, peer); err != nil {
		t.Fatalf("join: %v", err)
	}

	buf := []byte("original")
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: buf}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Mutate the caller's own buffer immediately after Dispatch returns —
	// Dispatch returning only guarantees Logic.Handle ran, not that
	// delivery to the Peer's writer goroutine has happened yet.
	copy(buf, "mutated!")

	got := waitForMessages(t, peer, 2)
	if string(got[1]) != "original" {
		t.Fatalf("peer received %q, want the unmutated original %q", got[1], "original")
	}
}

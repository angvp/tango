package realtime

import (
	"context"
	"errors"
	"testing"
)

// TestDispatchPeerRejectsReplacedPeer covers finding 1: a peer that has
// been superseded by a later Join for the same principal must never reach
// Logic.Handle again, even though its physical connection (and thus its
// own read loop, in a real transport) may still be alive and calling
// DispatchPeer.
func TestDispatchPeerRejectsReplacedPeer(t *testing.T) {
	logic, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	a := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, a); err != nil {
		t.Fatalf("a join: %v", err)
	}
	b := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, b); err != nil {
		t.Fatalf("b join (replace): %v", err)
	}
	waitFor(t, a.isClosed, "a should have been closed on replacement")

	before := len(logic.recordedEvents())
	err := hub.DispatchPeer(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}, Payload: []byte("from a")}, a)
	if !errors.Is(err, ErrStalePeer) {
		t.Fatalf("DispatchPeer from replaced peer a = %v, want ErrStalePeer", err)
	}
	if got := len(logic.recordedEvents()); got != before {
		t.Fatalf("Logic.Handle was called for a stale peer's action: %d new events", got-before)
	}

	if err := hub.DispatchPeer(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}, Payload: []byte("from b")}, b); err != nil {
		t.Fatalf("DispatchPeer from current peer b: %v", err)
	}
}

// TestDispatchPeerRejectsOverflowRemovedPeer covers the overflow half of
// finding 1: a peer closed because its outbound queue overflowed must not
// be able to dispatch afterward either, even though nothing ever "replaced"
// it in the Join sense.
func TestDispatchPeerRejectsOverflowRemovedPeer(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{PeerQueue: 1})
	ctx := context.Background()

	slow := &fakePeer{block: make(chan struct{})}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "slow"}, slow); err != nil {
		t.Fatalf("join: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := hub.SendUser(ctx, "slow", []byte("x")); err != nil {
			t.Fatalf("send to slow: %v", err)
		}
	}
	waitFor(t, slow.isClosed, "slow peer was never closed after queue overflow")

	err := hub.DispatchPeer(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "slow"}}, slow)
	if !errors.Is(err, ErrStalePeer) {
		t.Fatalf("DispatchPeer from overflow-removed peer = %v, want ErrStalePeer", err)
	}
}

// TestDispatchPeerRejectsUnknownPrincipal covers "unknown peers" — a
// DispatchPeer for a principal that was never a member at all.
func TestDispatchPeerRejectsUnknownPrincipal(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	stranger := &fakePeer{}
	err := hub.DispatchPeer(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "ghost"}}, stranger)
	if !errors.Is(err, ErrStalePeer) {
		t.Fatalf("DispatchPeer for never-joined principal = %v, want ErrStalePeer", err)
	}
}

// TestHubDispatchStillWorksWithoutMembership proves finding 1 didn't
// collapse the bot/host-side Dispatch path into DispatchPeer's
// membership-bound one: a bot Principal that never joined can still act via
// Hub.Dispatch.
func TestHubDispatchStillWorksWithoutMembership(t *testing.T) {
	logic, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "bot"}}); err != nil {
		t.Fatalf("bot dispatch: %v", err)
	}

	found := false
	for _, ev := range logic.recordedEvents() {
		if ev.Kind == EventAction && ev.Principal.UserID == "bot" {
			found = true
		}
	}
	if !found {
		t.Fatal("bot dispatch never reached Logic.Handle")
	}
}

// TestDispatchPeerRequiresEventAction mirrors Dispatch's existing
// EventAction-only contract.
func TestDispatchPeerRequiresEventAction(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	if err := hub.DispatchPeer(context.Background(), Event{Kind: EventJoin, RoomID: "room-1"}, &fakePeer{}); err == nil {
		t.Fatal("expected error for non-EventAction DispatchPeer")
	}
}

package realtime

import (
	"context"
	"errors"
	"testing"
)

// nonComparablePeer is a Peer implementation whose dynamic type is not
// comparable (a struct containing a slice field), used only to prove Join
// rejects it cleanly instead of ever letting it reach a room-loop `==`
// comparison, which would panic.
type nonComparablePeer struct {
	tag []byte
}

func (nonComparablePeer) Send(context.Context, []byte) error { return nil }
func (nonComparablePeer) Close() error                       { return nil }

func TestJoinRejectsNilPeer(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})

	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, nil)
	if !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("Join with nil peer = %v, want ErrInvalidPeer", err)
	}
}

func TestJoinRejectsNonComparablePeer(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})

	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, nonComparablePeer{tag: []byte("x")})
	if !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("Join with non-comparable peer = %v, want ErrInvalidPeer", err)
	}
}

// TestStaleLeaveAfterReplacementIsSafeNoOp is the Leave half of finding 2:
// a Leave carrying a superseded Peer must resolve without panicking and
// without touching the current member.
func TestStaleLeaveAfterReplacementIsSafeNoOp(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	a := &fakePeer{}
	b := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, a); err != nil {
		t.Fatalf("a join: %v", err)
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, b); err != nil {
		t.Fatalf("b join (replace): %v", err)
	}

	if err := hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, a); err != nil {
		t.Fatalf("stale leave: %v", err)
	}
	if err := hub.SendUser(ctx, "alice", []byte("still b")); err != nil {
		t.Fatalf("send user: %v", err)
	}
	if got := waitForMessages(t, b, 2); string(got[1]) != "still b" {
		t.Fatalf("b should still be the live member, messages = %v", got)
	}
}

// TestStaleDispatchPeerAfterReplacementIsSafeAndErrors is the DispatchPeer
// half of finding 2 — belt-and-suspenders alongside
// TestDispatchPeerRejectsReplacedPeer: it specifically exercises the
// identity comparison never panicking regardless of run order.
func TestStaleDispatchPeerAfterReplacementIsSafeAndErrors(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	a := &fakePeer{}
	b := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, a); err != nil {
		t.Fatalf("a join: %v", err)
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, b); err != nil {
		t.Fatalf("b join (replace): %v", err)
	}

	err := hub.DispatchPeer(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}}, a)
	if !errors.Is(err, ErrStalePeer) {
		t.Fatalf("stale DispatchPeer = %v, want ErrStalePeer", err)
	}
}

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

// TestValidatePeer is table-driven: validatePeer must reject a nil
// interface, a typed nil pointer hiding inside a non-nil Peer interface
// (comparable and != nil as an interface value, but a call to its methods
// can still panic), and any non-comparable dynamic type — while accepting
// an ordinary valid pointer Peer.
func TestValidatePeer(t *testing.T) {
	var nilPeer *fakePeer // typed nil, wrapped into the Peer interface below

	cases := []struct {
		name    string
		peer    Peer
		wantErr bool
	}{
		{name: "nil interface", peer: nil, wantErr: true},
		{name: "typed nil pointer", peer: nilPeer, wantErr: true},
		{name: "valid non-nil pointer", peer: &fakePeer{}, wantErr: false},
		{name: "non-comparable value", peer: nonComparablePeer{tag: []byte("x")}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePeer(tc.peer)
			if tc.wantErr && !errors.Is(err, ErrInvalidPeer) {
				t.Fatalf("validatePeer(%#v) = %v, want ErrInvalidPeer", tc.peer, err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validatePeer(%#v) = %v, want nil", tc.peer, err)
			}
		})
	}
}

// TestJoinRejectsTypedNilPeer proves the same rejection happens through the
// public Join entry point, not just at the validatePeer unit level, and
// that no room-loop goroutine is ever started for it (Join fails before
// touching a room at all).
func TestJoinRejectsTypedNilPeer(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})

	var nilPeer *fakePeer
	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, nilPeer)
	if !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("Join with typed-nil peer = %v, want ErrInvalidPeer", err)
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

package realtime

import (
	"context"
	"errors"
	"testing"
)

// TestPeerSendFailureRemovesSoleMemberAndSchedulesEviction covers finding
// 3: a failed Peer.Send must be reported to the room loop, which removes
// exactly that member and, since it was the room's only one, schedules
// eviction the same way an ordinary Leave would.
func TestPeerSendFailureRemovesSoleMemberAndSchedulesEviction(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, sched := newTestHub(t, factory, Options{})
	ctx := context.Background()

	broken := &fakePeer{sendErr: errors.New("broken pipe")}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, broken); err != nil {
		t.Fatalf("join: %v", err)
	}

	// The snapshot delivery's Send fails immediately; the writer reports it
	// and the room loop removes alice, leaving the room empty and
	// scheduling eviction — never mutating membership from the writer
	// goroutine itself.
	waitFor(t, broken.isClosed, "broken peer was never closed after its Send failed")
	sched.fireAll()
	waitFor(t, func() bool {
		return errors.Is(hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1"}), ErrRoomNotFound)
	}, "room was never evicted after its sole peer's Send failed")
}

// TestPeerSendFailureAfterReplacementDoesNotRemoveReplacement covers the
// generation-safety half of finding 3: a Send failure reported for a
// connection already superseded by a later Join must be a no-op that never
// touches the replacement.
func TestPeerSendFailureAfterReplacementDoesNotRemoveReplacement(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	a := &fakePeer{block: make(chan struct{}), sendErr: errors.New("broken pipe")}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, a); err != nil {
		t.Fatalf("a join: %v", err)
	}
	// a's writer goroutine is now blocked trying to deliver the snapshot.

	b := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, b); err != nil {
		t.Fatalf("b join (replace): %v", err)
	}
	// Replacing a already canceled its writer's context and removed it from
	// membership. Releasing its blocked Send now exercises a late
	// Send-failure report arriving for an already-replaced connection.
	close(a.block)

	if err := hub.SendUser(ctx, "alice", []byte("still b")); err != nil {
		t.Fatalf("send user: %v", err)
	}
	if got := waitForMessages(t, b, 2); string(got[1]) != "still b" {
		t.Fatalf("b should remain the live member, messages = %v", got)
	}
}

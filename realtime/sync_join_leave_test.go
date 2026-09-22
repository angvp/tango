package realtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestJoinDoesNotReturnBeforeEventJoinHandleFinishes covers finding 4: Join
// must block until Logic.Handle(EventJoin) has actually completed, not
// merely until membership/snapshot have been applied.
func TestJoinDoesNotReturnBeforeEventJoinHandleFinishes(t *testing.T) {
	reachedHandle := make(chan struct{})
	release := make(chan struct{})
	joinReturned := make(chan struct{})

	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventJoin {
			close(reachedHandle)
			<-release
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	go func() {
		_ = hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, &fakePeer{})
		close(joinReturned)
	}()

	<-reachedHandle
	select {
	case <-joinReturned:
		t.Fatal("Join returned before its EventJoin Handle call finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-joinReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Join never returned after its EventJoin Handle call finished")
	}
}

// TestJoinReturnsEventJoinHandlerError covers finding 4's error-propagation
// half: an EventJoin Handle error must reach the Join caller, with
// errors.Is compatibility, and must not roll back the membership/snapshot
// already applied.
func TestJoinReturnsEventJoinHandlerError(t *testing.T) {
	wantErr := errors.New("join rejected by logic")
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventJoin {
			return wantErr
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	peer := &fakePeer{}
	err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, peer)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Join error = %v, want %v", err, wantErr)
	}

	// Membership and the snapshot delivery already made are not rolled
	// back: alice is still reachable via SendUser.
	if err := hub.SendUser(ctx, "alice", []byte("still here")); err != nil {
		t.Fatalf("send user: %v", err)
	}
	if got := waitForMessages(t, peer, 2); string(got[1]) != "still here" {
		t.Fatalf("membership should not have been rolled back, messages = %v", got)
	}
}

// TestLeaveDoesNotReturnBeforeEventLeaveHandleFinishes is Leave's
// counterpart to TestJoinDoesNotReturnBeforeEventJoinHandleFinishes.
func TestLeaveDoesNotReturnBeforeEventLeaveHandleFinishes(t *testing.T) {
	reachedHandle := make(chan struct{})
	release := make(chan struct{})
	leaveReturned := make(chan struct{})

	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventLeave {
			close(reachedHandle)
			<-release
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	peer := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, peer); err != nil {
		t.Fatalf("join: %v", err)
	}

	go func() {
		_ = hub.Leave(context.Background(), "room-1", Principal{UserID: "alice"}, peer)
		close(leaveReturned)
	}()

	<-reachedHandle
	select {
	case <-leaveReturned:
		t.Fatal("Leave returned before its EventLeave Handle call finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-leaveReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Leave never returned after its EventLeave Handle call finished")
	}
}

// TestLeaveReturnsEventLeaveHandlerError covers Leave's error-propagation
// half of finding 4.
func TestLeaveReturnsEventLeaveHandlerError(t *testing.T) {
	wantErr := errors.New("leave rejected by logic")
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventLeave {
			return wantErr
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	peer := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, peer); err != nil {
		t.Fatalf("join: %v", err)
	}
	err := hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, peer)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Leave error = %v, want %v", err, wantErr)
	}
}

// TestStaleLeaveEmitsNoEventLeave documents the settled no-rollback rule: a
// stale Leave is an idempotent no-op that never reaches Logic.Handle at all
// — not even to have its (nonexistent) result discarded.
func TestStaleLeaveEmitsNoEventLeave(t *testing.T) {
	logic, factory := newRecordingFactory()
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
	for _, ev := range logic.recordedEvents() {
		if ev.Kind == EventLeave {
			t.Fatalf("stale leave must not emit EventLeave, got %+v", ev)
		}
	}
}

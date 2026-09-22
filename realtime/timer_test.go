package realtime

import (
	"context"
	"testing"
	"time"
)

func newTimerLogic() (*recordingLogic, chan Event) {
	events := make(chan Event, 16)
	logic := &recordingLogic{}
	logic.handle = func(rc *RoomContext, ev Event) error {
		switch ev.Kind {
		case EventAction:
			return rc.ResetTimer(string(ev.Payload), 5*time.Second)
		case EventTimer:
			events <- ev
		}
		return nil
	}
	return logic, events
}

func TestResetTimerBasicRoundTrip(t *testing.T) {
	logic, events := newTimerLogic()
	hub, sched := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("turn")}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	sched.fireAll()

	select {
	case ev := <-events:
		if ev.Timer != "turn" || ev.RoomID != "room-1" {
			t.Fatalf("timer event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected exactly one timer event")
	}
}

func TestResetTimerBeforeFireOnlyFiresOnce(t *testing.T) {
	logic, events := newTimerLogic()
	hub, sched := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("turn")}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("turn")}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	// The first ResetTimer's fake timer was Stop()'d by the second call, so
	// fireAll only actually fires the still-pending (second) one.
	sched.fireAll()

	select {
	case ev := <-events:
		if ev.Timer != "turn" {
			t.Fatalf("timer event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected exactly one timer event")
	}
	select {
	case ev := <-events:
		t.Fatalf("unexpected second timer event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTimerGenerationSafetyIgnoresStaleExpiry(t *testing.T) {
	logic, events := newTimerLogic()
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("turn")}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	r := hub.getRoom("room-1")
	staleGen := r.timers["turn"].generation

	// Reset again: bumps the generation, so staleGen is now behind.
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("turn")}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}

	// Simulate an old-generation expiry that was blocked (e.g. waiting for
	// room-queue capacity) and only now reaches the room loop, after
	// ResetTimer already moved the generation forward.
	r.submitInternal(roomOp{
		kind:     opTimerExpiry,
		timerGen: staleGen,
		event:    Event{Kind: EventTimer, RoomID: "room-1", Timer: "turn"},
	})

	select {
	case ev := <-events:
		t.Fatalf("stale-generation timer expiry should have been ignored, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMultipleNamedTimersAreIndependent(t *testing.T) {
	logic, events := newTimerLogic()
	hub, sched := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("t1")}); err != nil {
		t.Fatalf("dispatch t1: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("t2")}); err != nil {
		t.Fatalf("dispatch t2: %v", err)
	}
	sched.fireAll()

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-events:
			seen[ev.Timer] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for timer events, saw so far: %v", seen)
		}
	}
	if !seen["t1"] || !seen["t2"] {
		t.Fatalf("expected both timers to fire independently, saw %v", seen)
	}
}

func TestResetTimerRejectsNonPositiveDuration(t *testing.T) {
	var gotErr error
	logic := &recordingLogic{}
	logic.handle = func(rc *RoomContext, ev Event) error {
		gotErr = rc.ResetTimer("t", 0)
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gotErr == nil {
		t.Fatal("expected error for non-positive timer duration")
	}
}

func TestTimerDiscardedWhenRoomClosesDuringEnqueue(t *testing.T) {
	logic, _ := newTimerLogic()
	hub, sched := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Payload: []byte("turn")}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- hub.Close(context.Background()) }()
	sched.fireAll() // race the timer firing against Hub.Close tearing the room down

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hub.Close never completed — possible deadlock racing a firing timer")
	}
}

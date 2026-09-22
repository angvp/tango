package realtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCloseIsIdempotent(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}

	if err := hub.Close(ctx); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := hub.Close(ctx); err != nil {
		t.Fatalf("second sequential close: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = hub.Close(context.Background())
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent close %d: %v", i, err)
		}
	}
}

func TestCloseRejectsInFlightAndSubsequentWork(t *testing.T) {
	release := make(chan struct{})
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			<-release
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}

	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "room-1"})
	}()
	time.Sleep(20 * time.Millisecond) // let it reach Handle and block there

	closeDone := make(chan error, 1)
	go func() { closeDone <- hub.Close(context.Background()) }()
	time.Sleep(20 * time.Millisecond) // let Close begin (closeCh closed)

	if err := hub.Join(ctx, "room-2", Principal{UserID: "b"}, &fakePeer{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Join after Close began = %v, want ErrClosed", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Dispatch after Close began = %v, want ErrClosed", err)
	}

	close(release) // unblock the in-flight Handle call so shutdown can proceed

	select {
	case <-dispatchDone:
		// The in-flight dispatch may complete successfully (it was already
		// past the closed-check) or report closure — either is acceptable;
		// what matters is that it doesn't hang.
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight dispatch never returned")
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

func TestCloseStopsPendingTimers(t *testing.T) {
	fired := make(chan Event, 1)
	logic := &recordingLogic{}
	logic.handle = func(rc *RoomContext, ev Event) error {
		switch ev.Kind {
		case EventAction:
			return rc.ResetTimer("t", time.Second)
		case EventTimer:
			fired <- ev
		}
		return nil
	}
	hub, sched := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if err := hub.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	// By the time Close returned, shutdown had already Stop()'d the pending
	// timer, so firing the fake scheduler now must be a no-op.
	sched.fireAll()
	select {
	case ev := <-fired:
		t.Fatalf("timer should not have fired after Close, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCloseClosesEveryLivePeer(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	a := &fakePeer{}
	b := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, a); err != nil {
		t.Fatalf("join room-1: %v", err)
	}
	if err := hub.Join(ctx, "room-2", Principal{UserID: "b"}, b); err != nil {
		t.Fatalf("join room-2: %v", err)
	}

	if err := hub.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	waitFor(t, a.isClosed, "peer a should have been closed by Hub.Close")
	waitFor(t, b.isClosed, "peer b should have been closed by Hub.Close")
}

func TestCloseWaitsForRoomLoopToActuallyExit(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	r := hub.getRoom("room-1")

	if err := hub.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-r.done:
	default:
		t.Fatal("Close returned before the room loop actually exited (r.done not yet closed)")
	}
}

func TestCloseReturnsPromptlyOnContextCancellation(t *testing.T) {
	release := make(chan struct{})
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			<-release // keeps the room loop permanently busy for this test
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	go func() {
		_ = hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "room-1"})
	}()
	time.Sleep(20 * time.Millisecond) // let it reach Handle and block there

	shortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := hub.Close(shortCtx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %v to return after its context was canceled — should be prompt", elapsed)
	}

	close(release) // let the still-shutting-down room finish, avoid leaking it past the test
}

func TestJoinCannotCreateRoomAfterCloseSnapshot(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	if err := hub.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := hub.getOrCreateRoom("room-1"); ok {
		t.Fatal("getOrCreateRoom should refuse to create a room once the Hub is closed")
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "a"}, &fakePeer{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Join on a closed Hub = %v, want ErrClosed", err)
	}
}

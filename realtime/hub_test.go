package realtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeScheduler is a deterministic, test-only stand-in for time.AfterFunc:
// scheduled callbacks only fire when the test explicitly triggers them, so
// eviction/timer tests never depend on real wall-clock sleeps.
type fakeScheduler struct {
	mu      sync.Mutex
	pending []*fakeTimer
}

type fakeTimer struct {
	fire    func()
	stopped bool
}

func (s *fakeScheduler) after(_ time.Duration, f func()) func() bool {
	t := &fakeTimer{fire: f}
	s.mu.Lock()
	s.pending = append(s.pending, t)
	s.mu.Unlock()
	return func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if t.stopped {
			return false
		}
		t.stopped = true
		return true
	}
}

// fireAll synchronously fires every not-yet-stopped pending timer.
func (s *fakeScheduler) fireAll() {
	s.mu.Lock()
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, t := range pending {
		s.mu.Lock()
		stopped := t.stopped
		s.mu.Unlock()
		if !stopped {
			t.fire()
		}
	}
}

// waitFor polls cond, failing the test if it hasn't become true within a
// short bound — used only to observe an asynchronous side effect (like a
// fire-and-forget Peer.Close call) settling, never to await room-loop
// application of an op (which is synchronous and needs no polling).
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal(msg)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// waitForMessages polls until p has received at least n messages, since
// delivery to a Peer happens on a separate writer goroutine drained from
// its outbound queue — a room-loop op returning does not itself guarantee
// the message has reached Peer.Send yet.
func waitForMessages(t *testing.T, p *fakePeer, n int) [][]byte {
	t.Helper()
	var got [][]byte
	waitFor(t, func() bool {
		got = p.messages()
		return len(got) >= n
	}, "peer never received the expected number of messages")
	return got
}

func newTestHub(t *testing.T, factory Factory, opts Options) (*Hub, *fakeScheduler) {
	t.Helper()
	hub, err := NewHub(factory, opts)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	sched := &fakeScheduler{}
	hub.newTimer = sched.after
	return hub, sched
}

// fakePeer is a Peer whose received messages can be inspected by tests. A
// pointer to fakePeer is the comparable identity Join/Leave rely on.
type fakePeer struct {
	mu       sync.Mutex
	received [][]byte
	closed   bool
	// block, if non-nil, is closed to allow a queued Send to proceed —
	// used to simulate a slow/stalled peer.
	block chan struct{}
}

func (p *fakePeer) Send(ctx context.Context, payload []byte) error {
	if p.block != nil {
		select {
		case <-p.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.mu.Lock()
	p.received = append(p.received, payload)
	p.mu.Unlock()
	return nil
}

func (p *fakePeer) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return nil
}

func (p *fakePeer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *fakePeer) messages() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.received))
	copy(out, p.received)
	return out
}

// recordingLogic is a minimal, thread-unsafe-by-design Logic (only ever
// called from one room's own goroutine) that records every Event it
// receives and lets a test inject Handle/Snapshot behavior.
type recordingLogic struct {
	mu       sync.Mutex
	events   []Event
	snapshot func(Principal) ([]byte, error)
	handle   func(*RoomContext, Event) error
}

func (l *recordingLogic) Handle(rc *RoomContext, ev Event) error {
	l.mu.Lock()
	l.events = append(l.events, ev)
	l.mu.Unlock()
	if l.handle != nil {
		return l.handle(rc, ev)
	}
	return nil
}

func (l *recordingLogic) Snapshot(rc *RoomContext, p Principal) ([]byte, error) {
	if l.snapshot != nil {
		return l.snapshot(p)
	}
	return []byte("snapshot:" + p.UserID), nil
}

func (l *recordingLogic) recordedEvents() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

func newRecordingFactory() (*recordingLogic, Factory) {
	logic := &recordingLogic{}
	return logic, func(string) Logic { return logic }
}

func TestNewHubValidation(t *testing.T) {
	factory := func(string) Logic { return &recordingLogic{} }

	if _, err := NewHub(nil, Options{}); err == nil {
		t.Fatal("expected error for nil factory")
	}
	if _, err := NewHub(factory, Options{ReconnectWindow: -1}); err == nil {
		t.Fatal("expected error for negative ReconnectWindow")
	}
	if _, err := NewHub(factory, Options{RoomQueue: -1}); err == nil {
		t.Fatal("expected error for negative RoomQueue")
	}
	if _, err := NewHub(factory, Options{PeerQueue: -1}); err == nil {
		t.Fatal("expected error for negative PeerQueue")
	}

	hub, err := NewHub(factory, Options{})
	if err != nil {
		t.Fatalf("NewHub with zero Options: %v", err)
	}
	if hub.opts.ReconnectWindow != DefaultReconnectWindow || hub.opts.RoomQueue != DefaultRoomQueue || hub.opts.PeerQueue != DefaultPeerQueue {
		t.Fatalf("zero Options did not default: %+v", hub.opts)
	}
}

func TestJoinDeliversSnapshotAndDispatchBroadcasts(t *testing.T) {
	logic, factory := newRecordingFactory()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	alice := &fakePeer{}
	bob := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, alice); err != nil {
		t.Fatalf("alice join: %v", err)
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "bob"}, bob); err != nil {
		t.Fatalf("bob join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}, Payload: []byte("hi")}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if got := waitForMessages(t, alice, 1); string(got[0]) != "snapshot:alice" {
		t.Fatalf("alice snapshot = %v", got)
	}
	if got := waitForMessages(t, bob, 2); string(got[0]) != "snapshot:bob" || string(got[1]) != "hi" {
		t.Fatalf("bob messages = %v", got)
	}
}

func TestRoomContextBroadcastExceptAndSendUser(t *testing.T) {
	logic := &recordingLogic{}
	logic.handle = func(rc *RoomContext, ev Event) error {
		switch ev.Kind {
		case EventAction:
			if err := rc.BroadcastExcept(ev.Principal.UserID, ev.Payload); err != nil {
				return err
			}
			return rc.SendUser(ev.Principal.UserID, []byte("ack"))
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	alice := &fakePeer{}
	bob := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, alice); err != nil {
		t.Fatalf("alice join: %v", err)
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "bob"}, bob); err != nil {
		t.Fatalf("bob join: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}, Payload: []byte("hi")}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if got := waitForMessages(t, bob, 2); string(got[1]) != "hi" {
		t.Fatalf("bob (not excluded) should have received the broadcast, got %v", got)
	}
	// alice was excluded from the broadcast but does get the rc.SendUser
	// ack: snapshot, then "ack" — never the broadcast payload itself.
	if got := waitForMessages(t, alice, 2); string(got[1]) != "ack" {
		t.Fatalf("alice should only have gotten her ack, not the broadcast: %v", got)
	}

	if err := hub.SendUser(ctx, "bob", []byte("direct")); err != nil {
		t.Fatalf("send user: %v", err)
	}
	if got := waitForMessages(t, bob, 3); string(got[2]) != "direct" {
		t.Fatalf("bob direct message = %v", got)
	}
}

func TestJoinFailsCleanlyWhenSnapshotErrors(t *testing.T) {
	wantErr := errors.New("no such principal")
	logic := &recordingLogic{snapshot: func(p Principal) ([]byte, error) {
		if p.UserID == "denied" {
			return nil, wantErr
		}
		return []byte("ok"), nil
	}}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	peer := &fakePeer{}
	err := hub.Join(ctx, "room-1", Principal{UserID: "denied"}, peer)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Join error = %v, want %v", err, wantErr)
	}
	if len(peer.messages()) != 0 {
		t.Fatalf("peer should not have received anything: %v", peer.messages())
	}
	// No membership was installed: a SendUser to this principal is a no-op,
	// not a delivery.
	if err := hub.SendUser(ctx, "denied", []byte("x")); err != nil {
		t.Fatalf("send user: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if len(peer.messages()) != 0 {
		t.Fatalf("peer should still have received nothing: %v", peer.messages())
	}
}

func TestDispatchReturnsLogicError(t *testing.T) {
	wantErr := errors.New("rejected")
	logic := &recordingLogic{handle: func(*RoomContext, Event) error { return wantErr }}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dispatch error = %v, want %v", err, wantErr)
	}
}

func TestDispatchRejectsNonActionKind(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	if err := hub.Dispatch(context.Background(), Event{Kind: EventJoin, RoomID: "room-1"}); err == nil {
		t.Fatal("expected error for non-EventAction Dispatch")
	}
}

func TestGenerationSafeReplacement(t *testing.T) {
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
	waitFor(t, a.isClosed, "old peer a should have been closed on replacement")

	// Stale leave from the replaced peer must not remove b.
	if err := hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, a); err != nil {
		t.Fatalf("stale leave: %v", err)
	}
	if err := hub.SendUser(ctx, "alice", []byte("ping")); err != nil {
		t.Fatalf("send user: %v", err)
	}
	if got := waitForMessages(t, b, 2); string(got[1]) != "ping" {
		t.Fatalf("b should still be live member, messages = %v", got)
	}
}

func TestPeerQueueOverflowClosesOnlyThatPeer(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{PeerQueue: 1})
	ctx := context.Background()

	slow := &fakePeer{block: make(chan struct{})} // never unblocked: Send always pending
	fast := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "slow"}, slow); err != nil {
		t.Fatalf("slow join: %v", err)
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "fast"}, fast); err != nil {
		t.Fatalf("fast join: %v", err)
	}

	// slow's queue (depth 1) already holds its snapshot and the writer is
	// blocked delivering it, so the next broadcasts overflow slow's queue.
	for i := 0; i < 3; i++ {
		if err := hub.SendUser(ctx, "slow", []byte("x")); err != nil {
			t.Fatalf("send to slow: %v", err)
		}
	}

	waitFor(t, slow.isClosed, "slow peer was never closed after queue overflow")

	// The room loop and fast's delivery must still work.
	if err := hub.SendUser(ctx, "fast", []byte("still alive")); err != nil {
		t.Fatalf("send to fast: %v", err)
	}
	if got := waitForMessages(t, fast, 2); string(got[1]) != "still alive" {
		t.Fatalf("fast messages = %v", got)
	}
}

func TestDispatchBlocksUntilRoomLoopProcessesIt(t *testing.T) {
	// Handle blocks on release only for EventAction, so Join's own
	// internal EventJoin dispatch (fired from inside handleJoin) isn't
	// affected — only a real Dispatch call occupies the room loop.
	release := make(chan struct{})
	logic := &recordingLogic{}
	logic.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			<-release
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{RoomQueue: 1})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	// This Dispatch is picked up by the room loop and blocks inside Handle.
	go func() {
		_ = hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "room-1"})
	}()
	time.Sleep(20 * time.Millisecond) // let the first dispatch reach Handle

	// A second Dispatch must block waiting for the room loop (busy with the
	// first) to actually process it — proving Dispatch is synchronous on
	// room-loop application, not merely on successful enqueue.
	shortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := hub.Dispatch(shortCtx, Event{Kind: EventAction, RoomID: "room-1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded while room loop was busy, got %v", err)
	}
	close(release)
}

func TestImplicitLifecycleAndBotDispatch(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, sched := newTestHub(t, factory, Options{})
	ctx := context.Background()

	// Bot dispatch against a nonexistent room fails.
	err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "bot"}})
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}

	peer := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, peer); err != nil {
		t.Fatalf("join: %v", err)
	}

	// Bot can now dispatch without ever joining.
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "bot"}}); err != nil {
		t.Fatalf("bot dispatch: %v", err)
	}

	if err := hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, peer); err != nil {
		t.Fatalf("leave: %v", err)
	}
	// Room is now empty; fire the eviction timer.
	sched.fireAll()

	waitFor(t, func() bool {
		return errors.Is(hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1"}), ErrRoomNotFound)
	}, "room was never evicted")

	// A Leave carrying a Peer that doesn't match the current live one
	// (here: nil, standing in for a stale/replaced connection) is an
	// idempotent no-op — no error, and membership is untouched.
	carolPeer := &fakePeer{}
	if err := hub.Join(ctx, "room-2", Principal{UserID: "carol"}, carolPeer); err != nil {
		t.Fatalf("join room-2: %v", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-2", Principal: Principal{UserID: "bot"}}); err != nil {
		t.Fatalf("bot dispatch room-2: %v", err)
	}
	if err := hub.Leave(ctx, "room-2", Principal{UserID: "carol"}, nil); err != nil {
		t.Fatalf("mismatched-peer leave should be a safe no-op, got error: %v", err)
	}
	if err := hub.SendUser(ctx, "carol", []byte("still member")); err != nil {
		t.Fatalf("send to carol: %v", err)
	}
	if got := waitForMessages(t, carolPeer, 2); string(got[1]) != "still member" {
		t.Fatalf("carol should remain a member after mismatched-peer leave, messages = %v", got)
	}
}

func TestReconnectWithinWindowRestoresMembershipAndSnapshot(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, sched := newTestHub(t, factory, Options{})
	ctx := context.Background()

	first := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, first); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, first); err != nil {
		t.Fatalf("leave: %v", err)
	}

	// Reconnect before the eviction timer fires.
	second := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, second); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if got := waitForMessages(t, second, 1); string(got[0]) != "snapshot:alice" {
		t.Fatalf("rejoin snapshot = %v", got)
	}

	// The original eviction timer (from the first Leave) is stale now;
	// firing it must not evict the room out from under the reconnected peer.
	sched.fireAll()
	if err := hub.SendUser(ctx, "alice", []byte("still here")); err != nil {
		t.Fatalf("send after stale eviction fire: %v", err)
	}
	if got := waitForMessages(t, second, 2); string(got[1]) != "still here" {
		t.Fatalf("expected room to survive stale eviction fire, messages = %v", got)
	}
}

func TestErrClosedAfterHubClose(t *testing.T) {
	_, factory := newRecordingFactory()
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := hub.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "bob"}, &fakePeer{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Join after Close = %v, want ErrClosed", err)
	}
	if err := hub.Dispatch(ctx, Event{Kind: EventAction, RoomID: "room-1"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Dispatch after Close = %v, want ErrClosed", err)
	}
}

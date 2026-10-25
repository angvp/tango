package realtime

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// chanPeer is a Peer that publishes each successful Send on recv and its
// Close on closed, so tests synchronise on delivery and teardown through
// channels rather than polling.
type chanPeer struct {
	recv   chan []byte
	closed chan struct{}
	once   sync.Once
}

func newChanPeer() *chanPeer {
	return &chanPeer{recv: make(chan []byte, 16), closed: make(chan struct{})}
}

func (p *chanPeer) Send(_ context.Context, payload []byte) error {
	p.recv <- payload
	return nil
}

func (p *chanPeer) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

// parkingLogic parks the room loop inside Handle for every EventAction, until
// gate is closed. entered signals that the loop is parked, so a test can fill
// the inbox knowing nothing drains it until release.
type parkingLogic struct {
	*recordingLogic
	entered chan struct{}
	gate    chan struct{}
}

func newParkingLogic() *parkingLogic {
	l := &parkingLogic{recordingLogic: &recordingLogic{}, entered: make(chan struct{}, 1), gate: make(chan struct{})}
	l.handle = func(_ *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			l.entered <- struct{}{}
			<-l.gate
		}
		return nil
	}
	return l
}

// parkRoomLoop parks roomID's loop inside Logic.Handle and occupies its single
// inbox slot with a filler op. The returned release unblocks Handle, lets the
// parked Dispatch return, and is also registered as cleanup.
func parkRoomLoop(t *testing.T, hub *Hub, roomID string, logic *parkingLogic) (filler chan error, release func()) {
	t.Helper()
	parked := make(chan error, 1)
	go func() {
		parked <- hub.Dispatch(context.Background(), actionEvent(roomID, "park"))
	}()
	<-logic.entered
	r := hub.getRoom(roomID)
	if r == nil {
		t.Fatalf("room %q is not tracked while its loop is parked", roomID)
	}
	filler = make(chan error, 1)
	r.inbox <- roomOp{kind: opSendUser, reply: filler}

	var once sync.Once
	release = func() {
		once.Do(func() {
			close(logic.gate)
			if err := <-parked; err != nil {
				t.Errorf("parked dispatch returned %v, want nil", err)
			}
		})
	}
	t.Cleanup(release)
	return filler, release
}

// mustReturn runs fn and fails if it does not return within a generous bound.
// The bound only converts a regression that would hang the test binary into a
// failure; correct behaviour never depends on it.
func mustReturn(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", name)
	}
}

func actionEvent(roomID, payload string) Event {
	return Event{Kind: EventAction, RoomID: roomID, Principal: Principal{UserID: "actor"}, Payload: []byte(payload)}
}

func newEdgeHub(t *testing.T, factory Factory, opts Options) (*Hub, *fakeScheduler) {
	t.Helper()
	hub, sched := newTestHub(t, factory, opts)
	t.Cleanup(func() { _ = hub.Close(context.Background()) })
	return hub, sched
}

func TestDefaultNewTimerStopReportsWhetherCallbackWasCancelled(t *testing.T) {
	t.Run("stop before expiry cancels the callback", func(t *testing.T) {
		stop := defaultNewTimer(time.Hour, func() {})
		if got := stop(); !got {
			t.Fatalf("first stop() = %v, want true", got)
		}
		if got := stop(); got {
			t.Fatalf("second stop() = %v, want false", got)
		}
	})

	t.Run("stop after expiry reports false", func(t *testing.T) {
		fired := make(chan struct{})
		stop := defaultNewTimer(0, func() { close(fired) })
		<-fired
		if got := stop(); got {
			t.Fatalf("stop() after the callback ran = %v, want false", got)
		}
	})
}

func TestLeaveForUnknownOrSupersededMembershipIsNoOp(t *testing.T) {
	type target struct {
		roomID    string
		principal Principal
		peer      Peer
	}
	cases := []struct {
		name string
		// setup joins alice to room-1 and returns the live peer plus the
		// Leave call that must be a silent no-op.
		setup func(t *testing.T, hub *Hub) (live *chanPeer, leave target)
	}{
		{
			name: "room that was never created",
			setup: func(t *testing.T, hub *Hub) (*chanPeer, target) {
				alice := newChanPeer()
				mustJoin(t, hub, "room-1", "alice", alice)
				return alice, target{roomID: "room-404", principal: Principal{UserID: "alice"}, peer: alice}
			},
		},
		{
			name: "user with no membership in an existing room",
			setup: func(t *testing.T, hub *Hub) (*chanPeer, target) {
				alice := newChanPeer()
				mustJoin(t, hub, "room-1", "alice", alice)
				return alice, target{roomID: "room-1", principal: Principal{UserID: "bob"}, peer: newChanPeer()}
			},
		},
		{
			name: "peer superseded by a later join",
			setup: func(t *testing.T, hub *Hub) (*chanPeer, target) {
				stale := newChanPeer()
				mustJoin(t, hub, "room-1", "alice", stale)
				fresh := newChanPeer()
				mustJoin(t, hub, "room-1", "alice", fresh)
				return fresh, target{roomID: "room-1", principal: Principal{UserID: "alice"}, peer: stale}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logic := &recordingLogic{handle: func(rc *RoomContext, ev Event) error {
				if ev.Kind == EventAction {
					return rc.Broadcast(ev.Payload)
				}
				return nil
			}}
			hub, _ := newEdgeHub(t, func(string) Logic { return logic }, Options{})
			live, leave := tc.setup(t, hub)

			if got := string(<-live.recv); got != "snapshot:alice" {
				t.Fatalf("live peer first delivery %q, want the join snapshot", got)
			}
			if err := hub.Leave(context.Background(), leave.roomID, leave.principal, leave.peer); err != nil {
				t.Fatalf("Leave = %v, want nil", err)
			}
			if leave.roomID == "room-404" && hub.getRoom("room-404") != nil {
				t.Fatal("Leave on an unknown room created it")
			}
			if err := hub.Dispatch(context.Background(), actionEvent("room-1", "ping")); err != nil {
				t.Fatalf("Dispatch after no-op Leave = %v, want nil", err)
			}
			if got := string(<-live.recv); got != "ping" {
				t.Fatalf("live peer received %q, want %q", got, "ping")
			}
			for _, ev := range logic.recordedEvents() {
				if ev.Kind == EventLeave {
					t.Fatalf("no-op Leave emitted an EventLeave for %+v", ev.Principal)
				}
			}
		})
	}
}

// TestLeaveWhileHubClosesIsNoOpAndQueuedOpsSeeErrClosed covers Leave when the
// hub closes while its room's inbox is full: the submit observes closeCh, and
// Leave maps ErrClosed to a no-op.
func TestLeaveWhileHubClosesIsNoOpAndQueuedOpsSeeErrClosed(t *testing.T) {
	logic := newParkingLogic()
	hub, _ := newEdgeHub(t, func(string) Logic { return logic }, Options{RoomQueue: 1})
	alice := newChanPeer()
	mustJoin(t, hub, "room-1", "alice", alice)
	filler, release := parkRoomLoop(t, hub, "room-1", logic)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := hub.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close with a canceled context = %v, want context.Canceled", err)
	}
	if err := hub.Leave(context.Background(), "room-1", Principal{UserID: "alice"}, alice); err != nil {
		t.Fatalf("Leave during close = %v, want nil", err)
	}

	release()
	if err := <-filler; !errors.Is(err, ErrClosed) {
		t.Fatalf("queued op reply = %v, want ErrClosed", err)
	}
	for _, ev := range logic.recordedEvents() {
		if ev.Kind == EventLeave {
			t.Fatal("Leave rejected by a closing hub still reached Logic.Handle")
		}
	}
	<-alice.closed
}

// TestSubmitWithCanceledContextOnFullInboxReturnsContextError covers submit's
// ctx.Done branch: with the inbox full and the hub open, the only ready case
// is the canceled context.
func TestSubmitWithCanceledContextOnFullInboxReturnsContextError(t *testing.T) {
	cases := []struct {
		name string
		call func(ctx context.Context, hub *Hub, peer Peer) error
	}{
		{"Dispatch", func(ctx context.Context, hub *Hub, _ Peer) error {
			return hub.Dispatch(ctx, actionEvent("room-1", "x"))
		}},
		{"DispatchPeer", func(ctx context.Context, hub *Hub, peer Peer) error {
			return hub.DispatchPeer(ctx, actionEvent("room-1", "x"), peer)
		}},
		{"SendUser", func(ctx context.Context, hub *Hub, _ Peer) error {
			return hub.SendUser(ctx, "alice", []byte("x"))
		}},
		{"Leave", func(ctx context.Context, hub *Hub, peer Peer) error {
			return hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, peer)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logic := newParkingLogic()
			hub, _ := newEdgeHub(t, func(string) Logic { return logic }, Options{RoomQueue: 1})
			alice := newChanPeer()
			mustJoin(t, hub, "room-1", "alice", alice)
			_, release := parkRoomLoop(t, hub, "room-1", logic)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := tc.call(ctx, hub, alice); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s with a canceled context on a full inbox = %v, want context.Canceled", tc.name, err)
			}
			release()
		})
	}
}

func TestDispatchPeerRejectsNilPeerAndUnknownRoom(t *testing.T) {
	cases := []struct {
		name    string
		roomID  string
		peer    Peer
		wantErr error
	}{
		{name: "nil peer is stale even for a live room", roomID: "room-1", peer: nil, wantErr: ErrStalePeer},
		{name: "unknown room", roomID: "room-404", peer: newChanPeer(), wantErr: ErrRoomNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub, _ := newEdgeHub(t, func(string) Logic { return &recordingLogic{} }, Options{})
			mustJoin(t, hub, "room-1", "alice", newChanPeer())

			err := hub.DispatchPeer(context.Background(), actionEvent(tc.roomID, "x"), tc.peer)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DispatchPeer = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestEvictionCheckRespectsMembersPresentWhenItFires pairs the regained-member
// case with an empty-room control, so the same instrument shows both outcomes.
// The regained member is injected directly: Join always cancels a pending
// eviction first, so the matching-generation, non-empty state is not reachable
// through the public API.
func TestEvictionCheckRespectsMembersPresentWhenItFires(t *testing.T) {
	cases := []struct {
		name         string
		regainMember bool
		// superseded fires the eviction callback captured before a later join,
		// which is the in-flight fire that Stop could not cancel.
		superseded bool
		wantErr    error
	}{
		{name: "empty room at fire time is evicted", wantErr: ErrRoomNotFound},
		{name: "room that regained a member survives", regainMember: true, wantErr: nil},
		{name: "fire superseded by a later join is ignored", superseded: true, wantErr: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub, sched := newEdgeHub(t, func(string) Logic { return &recordingLogic{} }, Options{})
			alice := newChanPeer()
			mustJoin(t, hub, "room-1", "alice", alice)
			if err := hub.Leave(context.Background(), "room-1", Principal{UserID: "alice"}, alice); err != nil {
				t.Fatalf("Leave = %v, want nil", err)
			}
			switch {
			case tc.superseded:
				inFlight := sched.pending[len(sched.pending)-1].fire
				mustJoin(t, hub, "room-1", "bob", newChanPeer())
				inFlight()
			case tc.regainMember:
				r := hub.getRoom("room-1")
				bob := newChanPeer()
				r.members["bob"] = &member{
					userID:    "bob",
					principal: Principal{UserID: "bob"},
					peer:      bob,
					queue:     make(chan []byte, 1),
					cancel:    func() {},
				}
				sched.fireAll()
			default:
				sched.fireAll()
			}
			// Dispatch is ordered behind the eviction check in the room inbox, so
			// its result reports the state the check left behind.
			err := hub.Dispatch(context.Background(), actionEvent("room-1", "after"))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Dispatch after the eviction check = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestSubmitInternalDiscardsOpWhenRoomOrHubIsGone covers both lifecycle cases
// of submitInternal: the hub closing while the inbox is full, and the room
// having already stopped while the inbox is full.
func TestSubmitInternalDiscardsOpWhenRoomOrHubIsGone(t *testing.T) {
	t.Run("hub closed while inbox is full", func(t *testing.T) {
		logic := newParkingLogic()
		hub, _ := newEdgeHub(t, func(string) Logic { return logic }, Options{RoomQueue: 1})
		mustJoin(t, hub, "room-1", "alice", newChanPeer())
		_, release := parkRoomLoop(t, hub, "room-1", logic)
		r := hub.getRoom("room-1")

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_ = hub.Close(canceled)

		mustReturn(t, "submitInternal after hub close", func() {
			r.submitInternal(roomOp{kind: opEvictCheck})
		})
		release()
	})

	t.Run("room stopped while inbox is full", func(t *testing.T) {
		hub, sched := newEdgeHub(t, func(string) Logic { return &recordingLogic{} }, Options{RoomQueue: 1})
		alice := newChanPeer()
		mustJoin(t, hub, "room-1", "alice", alice)
		r := hub.getRoom("room-1")
		if err := hub.Leave(context.Background(), "room-1", Principal{UserID: "alice"}, alice); err != nil {
			t.Fatalf("Leave = %v, want nil", err)
		}
		sched.fireAll()
		<-r.done

		r.inbox <- roomOp{kind: opEvictCheck}
		mustReturn(t, "submitInternal after room stop", func() {
			r.submitInternal(roomOp{kind: opEvictCheck})
		})
	})
}

// framingClassEncoder selects the Framer result for a single delivery by the
// payload it is given: "err" fails, "nil" returns nil, "empty" returns an empty
// slice. Any other payload frames as "<seq>:<payload>".
func framingClassEncoder(_ Principal, seq uint64, payload []byte) ([]byte, error) {
	switch string(payload) {
	case "err":
		return nil, errFrameRejected
	case "nil":
		return nil, nil
	case "empty":
		return []byte{}, nil
	}
	return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
}

var errFrameRejected = errors.New("test: frame rejected")

func countFramesWithPayload(calls []frameCall, payload string) int {
	n := 0
	for _, c := range calls {
		if string(c.payload) == payload {
			n++
		}
	}
	return n
}

// TestFramerResultOnEveryDeliveryPath proves that a Framer error closes only the
// member it failed for, and that a nil or empty result consumes no sequence
// number, on each delivery path: Broadcast, BroadcastExcept, SendUser, and a
// SendUser issued from a timer expiry.
func TestFramerResultOnEveryDeliveryPath(t *testing.T) {
	paths := []struct {
		name            string
		timer           bool
		send            func(rc *RoomContext, payload []byte) error
		aliceClassCalls int
	}{
		{name: "Broadcast", send: func(rc *RoomContext, p []byte) error { return rc.Broadcast(p) }, aliceClassCalls: 1},
		{name: "BroadcastExcept", send: func(rc *RoomContext, p []byte) error { return rc.BroadcastExcept("alice", p) }, aliceClassCalls: 0},
		{name: "SendUser", send: func(rc *RoomContext, p []byte) error { return rc.SendUser("bob", p) }, aliceClassCalls: 0},
		{name: "timer SendUser", timer: true, send: func(rc *RoomContext, p []byte) error { return rc.SendUser("bob", p) }, aliceClassCalls: 0},
	}
	results := []struct {
		name     string
		payload  string
		bobSeqs  []uint64
		bobGetOK bool
	}{
		{name: "framer error closes the member", payload: "err", bobSeqs: []uint64{1, 2}, bobGetOK: false},
		{name: "framer nil consumes no sequence", payload: "nil", bobSeqs: []uint64{1, 2, 2}, bobGetOK: true},
		{name: "framer empty consumes no sequence", payload: "empty", bobSeqs: []uint64{1, 2, 2}, bobGetOK: true},
	}

	for _, p := range paths {
		for _, res := range results {
			t.Run(p.name+"/"+res.name, func(t *testing.T) {
				logic := newFramingLogic()
				logic.encode = framingClassEncoder
				logic.handle = func(rc *RoomContext, ev Event) error {
					switch {
					case ev.Kind == EventTimer:
						return p.send(rc, []byte(res.payload))
					case ev.Kind != EventAction:
						return nil
					case string(ev.Payload) == "arm":
						return rc.ResetTimer("t", time.Hour)
					default:
						return p.send(rc, ev.Payload)
					}
				}
				hub, sched := newEdgeHub(t, func(string) Logic { return logic }, Options{})
				alice, bob := newChanPeer(), newChanPeer()
				mustJoin(t, hub, "room-1", "alice", alice)
				mustJoin(t, hub, "room-1", "bob", bob)

				ctx := context.Background()
				if p.timer {
					if err := hub.Dispatch(ctx, actionEvent("room-1", "arm")); err != nil {
						t.Fatalf("arm timer: %v", err)
					}
					sched.fireAll()
				} else if err := hub.Dispatch(ctx, actionEvent("room-1", res.payload)); err != nil {
					t.Fatalf("dispatch: %v", err)
				}
				if err := hub.Dispatch(ctx, actionEvent("room-1", "ok")); err != nil {
					t.Fatalf("dispatch ok: %v", err)
				}

				// Dispatch returns only after the room loop has framed everything
				// it would frame, so Frame calls are read synchronously here.
				bobCalls := logic.framedCallsFor("bob")
				var gotSeqs []uint64
				for _, c := range bobCalls {
					gotSeqs = append(gotSeqs, c.seq)
				}
				if len(gotSeqs) != len(res.bobSeqs) {
					t.Fatalf("bob Frame sequences = %v, want %v", gotSeqs, res.bobSeqs)
				}
				for i := range gotSeqs {
					if gotSeqs[i] != res.bobSeqs[i] {
						t.Fatalf("bob Frame sequences = %v, want %v", gotSeqs, res.bobSeqs)
					}
				}
				if got := countFramesWithPayload(logic.framedCallsFor("alice"), res.payload); got != p.aliceClassCalls {
					t.Fatalf("alice framed %q %d times, want %d", res.payload, got, p.aliceClassCalls)
				}

				if res.bobGetOK {
					if got := string(<-bob.recv); got != "1:snapshot:bob" {
						t.Fatalf("bob first delivery = %q, want %q", got, "1:snapshot:bob")
					}
					if got := string(<-bob.recv); got != "2:ok" {
						t.Fatalf("bob second delivery = %q, want %q", got, "2:ok")
					}
					return
				}
				<-bob.closed
			})
		}
	}
}

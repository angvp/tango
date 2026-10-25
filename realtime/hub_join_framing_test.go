package realtime

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// The join-snapshot tests below drive Hub.Join on an opted-in room. The
// replacement snapshot is framed as sequence 1 before the existing member for
// that user is closed, so a failure while framing it leaves that member live.
// Sends that Logic.Snapshot makes through RoomContext are ordinary deliveries
// to the existing member: a failed join does not undo them, and a failure of
// one of them can close that member before the join proceeds.

func TestFramedJoinSnapshotIsSequenceOneAndJoinEventSendsFollow(t *testing.T) {
	logic := newFramingLogic()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventJoin {
			return rc.SendUser(ev.Principal.UserID, []byte("welcome"))
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	alice := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice)

	assertMessages(t, waitForMessages(t, alice, 2), []string{"1:snapshot:alice", "2:welcome"})
	assertFrameCalls(t, logic.framedCallsFor("alice"), []string{"snapshot:alice", "welcome"}, []uint64{1, 2})
}

// joinFixture starts a framed room with alice already joined on alice. The
// logic broadcasts every action it receives, so a later dispatch reaches
// every member and the counters are visible in the peers' messages.
func joinFixture(t *testing.T, logic *framingLogic) (*Hub, *fakePeer) {
	t.Helper()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	alice := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice)
	return hub, alice
}

// assertFailedJoinLeftExistingLive is the shared post-condition for every
// failed join. The replacement join must fail before it touches the existing
// member, so a later broadcast still reaches that member on its own counter.
func assertFailedJoinLeftExistingLive(t *testing.T, hub *Hub, logic *framingLogic, existing, replacement *fakePeer) {
	t.Helper()
	if existing.isClosed() {
		t.Fatal("existing member was closed by a failed join")
	}
	dispatch(t, hub, "room-1", "alice", "later")
	assertMessages(t, waitForMessages(t, existing, 2), []string{"1:snapshot:alice", "2:later"})
	if existing.isClosed() {
		t.Fatal("existing member was closed after a failed join")
	}
	if n := len(replacement.messages()); n != 0 {
		t.Fatalf("replacement peer received %d messages from a failed join, want 0", n)
	}
	if n := countEvents(logic, EventJoin, "alice"); n != 1 {
		t.Fatalf("EventJoin handled %d times for alice, want 1 (the failed join must not handle it)", n)
	}
}

func countEvents(logic *framingLogic, kind EventKind, userID string) int {
	n := 0
	for _, ev := range logic.recordedEvents() {
		if ev.Kind == kind && ev.Principal.UserID == userID {
			n++
		}
	}
	return n
}

func TestJoinSnapshotErrorLeavesExistingMemberLive(t *testing.T) {
	wantErr := errors.New("snapshot refused")
	logic := newFramingLogic()
	failSnapshot := false
	logic.snapshot = func(p Principal) ([]byte, error) {
		if failSnapshot {
			return nil, wantErr
		}
		return []byte("snapshot:" + p.UserID), nil
	}
	hub, alice := joinFixture(t, logic)

	failSnapshot = true
	replacement := &fakePeer{}
	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, replacement)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Join error = %v, want %v", err, wantErr)
	}
	if got := logic.framedCallsFor("alice"); len(got) != 1 {
		t.Fatalf("Frame calls for alice = %d, want 1 (only the original snapshot): the failed snapshot must not be framed", len(got))
	}
	assertFailedJoinLeftExistingLive(t, hub, logic, alice, replacement)
}

func TestJoinFrameErrorLeavesExistingMemberLive(t *testing.T) {
	errFrame := errors.New("encoder refused")
	logic := newFramingLogic()
	failSnapshot := false
	logic.encode = func(_ Principal, seq uint64, payload []byte) ([]byte, error) {
		if failSnapshot && seq == 1 {
			return nil, errFrame
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	hub, alice := joinFixture(t, logic)

	failSnapshot = true
	replacement := &fakePeer{}
	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, replacement)
	if !errors.Is(err, errFrame) {
		t.Fatalf("Join error = %v, want it to wrap %v", err, errFrame)
	}
	assertFailedJoinLeftExistingLive(t, hub, logic, alice, replacement)
}

func TestJoinNilFramedSnapshotLeavesExistingMemberLive(t *testing.T) {
	logic := newFramingLogic()
	failSnapshot := false
	logic.encode = func(_ Principal, seq uint64, payload []byte) ([]byte, error) {
		if failSnapshot && seq == 1 {
			return nil, nil
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	hub, alice := joinFixture(t, logic)

	failSnapshot = true
	replacement := &fakePeer{}
	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, replacement)
	if !errors.Is(err, ErrEmptyFrame) {
		t.Fatalf("Join error = %v, want ErrEmptyFrame", err)
	}
	assertFailedJoinLeftExistingLive(t, hub, logic, alice, replacement)
}

func TestJoinEmptyFramedSnapshotLeavesExistingMemberLive(t *testing.T) {
	logic := newFramingLogic()
	failSnapshot := false
	logic.encode = func(_ Principal, seq uint64, payload []byte) ([]byte, error) {
		if failSnapshot && seq == 1 {
			return []byte{}, nil
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	hub, alice := joinFixture(t, logic)

	failSnapshot = true
	replacement := &fakePeer{}
	err := hub.Join(context.Background(), "room-1", Principal{UserID: "alice"}, replacement)
	if !errors.Is(err, ErrEmptyFrame) {
		t.Fatalf("Join error = %v, want ErrEmptyFrame", err)
	}
	assertFailedJoinLeftExistingLive(t, hub, logic, alice, replacement)
}

func TestReconnectRestartsAtSequenceOneAndReplacedPeerGetsNoMoreFrames(t *testing.T) {
	logic := newFramingLogic()
	hub, alice1 := joinFixture(t, logic)
	ctx := context.Background()
	bob := &fakePeer{}
	mustJoin(t, hub, "room-1", "bob", bob)

	dispatch(t, hub, "room-1", "alice", "a")
	// Wait for alice1's writer to send "a" before replacing it: closing the
	// member cancels its writer, which may drop a frame still queued.
	assertMessages(t, waitForMessages(t, alice1, 2), []string{"1:snapshot:alice", "2:a"})

	alice2 := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, alice2); err != nil {
		t.Fatalf("replacement join: %v", err)
	}
	waitFor(t, alice1.isClosed, "replaced peer was not closed")
	dispatch(t, hub, "room-1", "alice", "b")

	assertMessages(t, waitForMessages(t, alice2, 2), []string{"1:snapshot:alice", "2:b"})
	assertMessages(t, waitForMessages(t, bob, 3), []string{"1:snapshot:bob", "2:a", "3:b"})
	assertMessages(t, alice1.messages(), []string{"1:snapshot:alice", "2:a"})
	assertFrameCalls(t, logic.framedCallsFor("alice"),
		[]string{"snapshot:alice", "a", "snapshot:alice", "b"}, []uint64{1, 2, 1, 2})
}

func TestStalePeerLeaveAndDispatchNeverReachFrameOrReplacementCounter(t *testing.T) {
	logic := newFramingLogic()
	hub, stale := joinFixture(t, logic)
	ctx := context.Background()
	replacement := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, replacement); err != nil {
		t.Fatalf("replacement join: %v", err)
	}
	before := logic.frameCallCount()

	if err := hub.Leave(ctx, "room-1", Principal{UserID: "alice"}, stale); err != nil {
		t.Fatalf("stale leave: %v", err)
	}
	ev := Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}, Payload: []byte("stale")}
	if err := hub.DispatchPeer(ctx, ev, stale); !errors.Is(err, ErrStalePeer) {
		t.Fatalf("stale DispatchPeer error = %v, want ErrStalePeer", err)
	}
	if got := logic.frameCallCount(); got != before {
		t.Fatalf("stale operations reached Frame: %d calls, want %d", got, before)
	}

	dispatch(t, hub, "room-1", "alice", "next")
	assertMessages(t, waitForMessages(t, replacement, 2), []string{"1:snapshot:alice", "2:next"})
}

// snapshotSendLogic is a framingLogic whose Snapshot sends sendPayload to the
// joining user through RoomContext while armed, as a host may do to seed the
// joiner's state. A test arms it only for the replacement join.
type snapshotSendLogic struct {
	*framingLogic
	armed       bool
	sendPayload string
}

func (l *snapshotSendLogic) Snapshot(rc *RoomContext, p Principal) ([]byte, error) {
	if l.armed {
		if err := rc.SendUser(p.UserID, []byte(l.sendPayload)); err != nil {
			return nil, err
		}
	}
	return []byte("snapshot:" + p.UserID), nil
}

// joinWithSnapshotSend starts a framed room and joins alice on existing. Action
// events are broadcast, so later dispatches reach every member.
func joinWithSnapshotSend(t *testing.T, logic *snapshotSendLogic, opts Options, existing Peer) *Hub {
	t.Helper()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, opts)
	mustJoin(t, hub, "room-1", "alice", existing)
	return hub
}

// runSnapshotSendThenReplacementFails makes the replacement snapshot's framing
// return (replacementOut, replacementErr). The Snapshot send is framed as
// sequence 3 and succeeds, so the failed join must leave alice's sequence at 3
// and the next payload must be sequence 4.
func runSnapshotSendThenReplacementFails(t *testing.T, replacementOut []byte, replacementErr, wantErr error) {
	t.Helper()
	logic := &snapshotSendLogic{framingLogic: newFramingLogic()}
	failReplacement := false
	logic.encode = func(_ Principal, seq uint64, payload []byte) ([]byte, error) {
		if failReplacement && seq == 1 {
			return replacementOut, replacementErr
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	alice := &fakePeer{}
	hub := joinWithSnapshotSend(t, logic, Options{}, alice)
	ctx := context.Background()
	dispatch(t, hub, "room-1", "alice", "a")
	assertMessages(t, waitForMessages(t, alice, 2), []string{"1:snapshot:alice", "2:a"})

	logic.armed, logic.sendPayload = true, "during"
	failReplacement = true
	replacement := &fakePeer{}
	err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, replacement)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Join error = %v, want %v", err, wantErr)
	}
	if alice.isClosed() {
		t.Fatal("existing member was closed by a failed join")
	}
	assertMessages(t, waitForMessages(t, alice, 3), []string{"1:snapshot:alice", "2:a", "3:during"})

	dispatch(t, hub, "room-1", "alice", "later")
	assertMessages(t, waitForMessages(t, alice, 4), []string{"1:snapshot:alice", "2:a", "3:during", "4:later"})
	if alice.isClosed() {
		t.Fatal("existing member was closed after a failed join")
	}
	if n := len(replacement.messages()); n != 0 {
		t.Fatalf("replacement peer received %d messages from a failed join, want 0", n)
	}
	if n := countEvents(logic.framingLogic, EventJoin, "alice"); n != 1 {
		t.Fatalf("EventJoin handled %d times for alice, want 1 (the failed join must not handle it)", n)
	}
	assertFrameCalls(t, logic.framedCallsFor("alice"),
		[]string{"snapshot:alice", "a", "during", "snapshot:alice", "later"}, []uint64{1, 2, 3, 1, 4})
}

// A Snapshot send is framed and admitted before the replacement snapshot fails.
// The failed join leaves that delivery in place and the existing member live.
func TestSnapshotSendThenFailedReplacementFrameKeepsExistingMemberLive(t *testing.T) {
	errFrame := errors.New("replacement snapshot refused")
	runSnapshotSendThenReplacementFails(t, nil, errFrame, errFrame)
}

func TestSnapshotSendThenEmptyReplacementFrameKeepsExistingMemberLive(t *testing.T) {
	runSnapshotSendThenReplacementFails(t, []byte{}, nil, ErrEmptyFrame)
}

// A Frame error on the Snapshot send closes the existing member. Snapshot sees
// no error (RoomContext sends return nil), so Join proceeds and the replacement
// snapshot is framed as sequence 1 for the new member.
func TestSnapshotSendFrameErrorClosesExistingMemberAndJoinProceeds(t *testing.T) {
	errFrame := errors.New("send refused")
	logic := &snapshotSendLogic{framingLogic: newFramingLogic()}
	failDuring := false
	logic.encode = func(_ Principal, seq uint64, payload []byte) ([]byte, error) {
		if failDuring && string(payload) == "during" {
			return nil, errFrame
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	alice := &fakePeer{}
	hub := joinWithSnapshotSend(t, logic, Options{}, alice)
	ctx := context.Background()
	dispatch(t, hub, "room-1", "alice", "a")
	assertMessages(t, waitForMessages(t, alice, 2), []string{"1:snapshot:alice", "2:a"})

	logic.armed, logic.sendPayload = true, "during"
	failDuring = true
	replacement := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, replacement); err != nil {
		t.Fatalf("join returned %v, want nil: the replacement snapshot is framed and admitted", err)
	}
	waitFor(t, alice.isClosed, "existing member was not closed by the Snapshot send's Frame error")
	assertMessages(t, alice.messages(), []string{"1:snapshot:alice", "2:a"})

	dispatch(t, hub, "room-1", "alice", "later")
	assertMessages(t, waitForMessages(t, replacement, 2), []string{"1:snapshot:alice", "2:later"})
	if n := countEvents(logic.framingLogic, EventJoin, "alice"); n != 2 {
		t.Fatalf("EventJoin handled %d times for alice, want 2 (the replacement join completed)", n)
	}
	assertFrameCalls(t, logic.framedCallsFor("alice"),
		[]string{"snapshot:alice", "a", "during", "snapshot:alice", "later"}, []uint64{1, 2, 3, 1, 2})
}

// A queue overflow on the Snapshot send closes the existing member. The writer
// is parked inside Send on the snapshot, so the queue holds exactly its two
// slots; the Snapshot send is the third payload and overflows.
func TestSnapshotSendOverflowClosesExistingMemberAndJoinProceeds(t *testing.T) {
	logic := &snapshotSendLogic{framingLogic: newFramingLogic()}
	slow := &fakePeer{block: make(chan struct{}), entered: make(chan struct{}, 1)} // never released
	hub := joinWithSnapshotSend(t, logic, Options{PeerQueue: 2}, slow)
	ctx := context.Background()
	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow's writer never entered Send for the snapshot")
	}
	dispatch(t, hub, "room-1", "alice", "p1")
	dispatch(t, hub, "room-1", "alice", "p2")

	logic.armed, logic.sendPayload = true, "during"
	replacement := &fakePeer{}
	if err := hub.Join(ctx, "room-1", Principal{UserID: "alice"}, replacement); err != nil {
		t.Fatalf("join returned %v, want nil: overflow of the Snapshot send closes the existing member and the join proceeds", err)
	}
	waitFor(t, slow.isClosed, "existing member was not closed by the Snapshot send's overflow")
	if n := len(slow.messages()); n != 0 {
		t.Fatalf("overflowed member received %d messages, want 0 (its writer was parked in Send)", n)
	}

	dispatch(t, hub, "room-1", "alice", "later")
	assertMessages(t, waitForMessages(t, replacement, 2), []string{"1:snapshot:alice", "2:later"})
	if n := countEvents(logic.framingLogic, EventJoin, "alice"); n != 2 {
		t.Fatalf("EventJoin handled %d times for alice, want 2 (the replacement join completed)", n)
	}
	assertFrameCalls(t, logic.framedCallsFor("alice"),
		[]string{"snapshot:alice", "p1", "p2", "during", "snapshot:alice", "later"}, []uint64{1, 2, 3, 4, 1, 2})
}

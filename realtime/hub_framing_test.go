package realtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// firstFramedSequence is the sequence of the first payload framed after
// join. The join snapshot is sequence 1, so the first later payload is 2.
const firstFramedSequence uint64 = 2

// framingLogic opts its room in to framing. Frame records each call and
// then delegates to encode, which defaults to "<seq>:<payload>".
type framingLogic struct {
	*recordingLogic
	mu     sync.Mutex
	calls  []frameCall
	encode func(to Principal, seq uint64, payload []byte) ([]byte, error)
}

type frameCall struct {
	to      Principal
	seq     uint64
	payload []byte
}

var _ Framer = (*framingLogic)(nil)

func newFramingLogic() *framingLogic {
	return &framingLogic{recordingLogic: &recordingLogic{}}
}

func (l *framingLogic) Frame(to Principal, seq uint64, payload []byte) ([]byte, error) {
	l.mu.Lock()
	l.calls = append(l.calls, frameCall{to: to, seq: seq, payload: append([]byte(nil), payload...)})
	l.mu.Unlock()
	if l.encode != nil {
		return l.encode(to, seq, payload)
	}
	return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
}

// framedCallsFor returns the Frame calls made for userID, in call order.
func (l *framingLogic) framedCallsFor(userID string) []frameCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []frameCall
	for _, c := range l.calls {
		if c.to.UserID == userID {
			out = append(out, c)
		}
	}
	return out
}

func (l *framingLogic) frameCallCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

// expectedFrame is the wire bytes for the i-th framed payload (0-based) after
// join.
func expectedFrame(i int, payload string) string {
	return fmt.Sprintf("%d:%s", firstFramedSequence+uint64(i), payload)
}

// TestFrameReceivesTheJoinedPrincipal pins that Frame is handed the principal
// the member joined with. Principal currently carries only UserID, so this
// passes whether or not the member stores the full value; it guards the
// wiring if Principal gains fields.
func TestFrameReceivesTheJoinedPrincipal(t *testing.T) {
	logic := newFramingLogic()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.SendUser("alice", []byte("hi"))
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	joined := Principal{UserID: "alice"}
	alice := &fakePeer{}
	if err := hub.Join(ctx, "room-1", joined, alice); err != nil {
		t.Fatalf("join: %v", err)
	}
	dispatch(t, hub, "room-1", "alice", "go")
	assertMessages(t, waitForMessages(t, alice, 2), []string{"1:snapshot:alice", "2:hi"})

	calls := logic.framedCallsFor("alice")
	if len(calls) != 2 {
		t.Fatalf("Frame calls for alice = %d, want 2", len(calls))
	}
	for i, c := range calls {
		if c.to != joined {
			t.Fatalf("Frame call %d received principal %+v, want the joined %+v", i, c.to, joined)
		}
	}
}

func TestFramedMixedLegacySendsArriveConsecutively(t *testing.T) {
	logic := newFramingLogic()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind != EventAction {
			return nil
		}
		switch string(ev.Payload) {
		case "broadcast":
			return rc.Broadcast([]byte("b"))
		case "except":
			return rc.BroadcastExcept("alice", []byte("e"))
		case "room-user":
			return rc.SendUser("bob", []byte("r"))
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
	for _, payload := range []string{"broadcast", "except", "room-user"} {
		ev := Event{Kind: EventAction, RoomID: "room-1", Principal: Principal{UserID: "alice"}, Payload: []byte(payload)}
		if err := hub.Dispatch(ctx, ev); err != nil {
			t.Fatalf("dispatch %s: %v", payload, err)
		}
	}
	if err := hub.SendUser(ctx, "bob", []byte("h")); err != nil {
		t.Fatalf("hub send bob: %v", err)
	}
	if err := hub.SendUser(ctx, "alice", []byte("a")); err != nil {
		t.Fatalf("hub send alice: %v", err)
	}

	// The join snapshot is sequence 1; every later payload follows it.
	wantAlice := []string{"1:snapshot:alice", expectedFrame(0, "b"), expectedFrame(1, "a")}
	wantBob := []string{"1:snapshot:bob", expectedFrame(0, "b"), expectedFrame(1, "e"), expectedFrame(2, "r"), expectedFrame(3, "h")}
	assertMessages(t, waitForMessages(t, alice, len(wantAlice)), wantAlice)
	assertMessages(t, waitForMessages(t, bob, len(wantBob)), wantBob)
}

func TestFramedCountersAreIndependentPerMemberAndPerRoom(t *testing.T) {
	factory := func(string) Logic {
		logic := newFramingLogic()
		logic.handle = func(rc *RoomContext, ev Event) error {
			if ev.Kind == EventAction {
				return rc.Broadcast(ev.Payload)
			}
			return nil
		}
		return logic
	}
	hub, _ := newTestHub(t, factory, Options{})
	ctx := context.Background()

	alice1 := &fakePeer{} // alice in room-1
	alice2 := &fakePeer{} // alice in room-2
	bob := &fakePeer{}
	carol := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice1)
	mustJoin(t, hub, "room-1", "bob", bob)
	mustJoin(t, hub, "room-2", "carol", carol)
	mustJoin(t, hub, "room-2", "alice", alice2)

	dispatch(t, hub, "room-1", "alice", "a1")
	dispatch(t, hub, "room-2", "carol", "b1")
	dispatch(t, hub, "room-1", "alice", "a2")
	if err := hub.SendUser(ctx, "alice", []byte("z")); err != nil {
		t.Fatalf("hub send alice: %v", err)
	}

	assertMessages(t, waitForMessages(t, alice1, 4), []string{"1:snapshot:alice", expectedFrame(0, "a1"), expectedFrame(1, "a2"), expectedFrame(2, "z")})
	assertMessages(t, waitForMessages(t, bob, 3), []string{"1:snapshot:bob", expectedFrame(0, "a1"), expectedFrame(1, "a2")})
	assertMessages(t, waitForMessages(t, alice2, 3), []string{"1:snapshot:alice", expectedFrame(0, "b1"), expectedFrame(1, "z")})
	assertMessages(t, waitForMessages(t, carol, 2), []string{"1:snapshot:carol", expectedFrame(0, "b1")})
}

func TestFramedEmptyResultAndExcludedMembersConsumeNoSequence(t *testing.T) {
	logic := newFramingLogic()
	logic.encode = func(_ Principal, seq uint64, payload []byte) ([]byte, error) {
		switch string(payload) {
		case "skip":
			return nil, nil
		case "empty":
			return []byte{}, nil
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind != EventAction {
			return nil
		}
		if string(ev.Payload) == "except" {
			return rc.BroadcastExcept("alice", []byte("e"))
		}
		return rc.Broadcast(ev.Payload)
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})
	ctx := context.Background()

	alice := &fakePeer{}
	bob := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice)
	mustJoin(t, hub, "room-1", "bob", bob)

	dispatch(t, hub, "room-1", "alice", "skip")
	dispatch(t, hub, "room-1", "alice", "empty")
	dispatch(t, hub, "room-1", "alice", "x")
	dispatch(t, hub, "room-1", "alice", "except")
	if err := hub.SendUser(ctx, "alice", []byte("y")); err != nil {
		t.Fatalf("hub send alice: %v", err)
	}
	// A user with no membership in the room is never passed to Frame.
	if err := hub.SendUser(ctx, "nobody", []byte("n")); err != nil {
		t.Fatalf("hub send nobody: %v", err)
	}

	assertMessages(t, waitForMessages(t, alice, 3), []string{"1:snapshot:alice", expectedFrame(0, "x"), expectedFrame(1, "y")})
	assertMessages(t, waitForMessages(t, bob, 3), []string{"1:snapshot:bob", expectedFrame(0, "x"), expectedFrame(1, "e")})

	// Frame calls are synchronous with the room loop, so every call for the
	// sends above has returned by now. Each member's snapshot is its first call.
	assertFrameCalls(t, logic.framedCallsFor("alice"), []string{"snapshot:alice", "skip", "empty", "x", "y"}, []uint64{1, 2, 2, 2, 3})
	assertFrameCalls(t, logic.framedCallsFor("bob"), []string{"snapshot:bob", "skip", "empty", "x", "e"}, []uint64{1, 2, 2, 2, 3})
	if got := logic.frameCallCount(); got != 10 {
		t.Fatalf("total Frame calls = %d, want 10 (nobody must not be passed to Frame)", got)
	}
}

func TestFramedEncoderErrorClosesOnlyThatMemberAndConsumesNoSequence(t *testing.T) {
	errEncode := errors.New("encoder refused")
	logic := newFramingLogic()
	logic.encode = func(to Principal, seq uint64, payload []byte) ([]byte, error) {
		if to.UserID == "alice" && string(payload) == "boom" {
			return nil, errEncode
		}
		return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
	}
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	alice := &fakePeer{}
	bob := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice)
	mustJoin(t, hub, "room-1", "bob", bob)

	dispatch(t, hub, "room-1", "bob", "x")
	// Wait for alice's writer to send "x" before closing her: closing a
	// member cancels its writer, which may drop a frame still queued.
	assertMessages(t, waitForMessages(t, alice, 2), []string{"1:snapshot:alice", expectedFrame(0, "x")})
	dispatch(t, hub, "room-1", "bob", "boom")
	dispatch(t, hub, "room-1", "bob", "after")

	waitFor(t, alice.isClosed, "alice was not closed after an encoder error")
	assertMessages(t, waitForMessages(t, bob, 4), []string{"1:snapshot:bob", expectedFrame(0, "x"), expectedFrame(1, "boom"), expectedFrame(2, "after")})
	// alice is no longer a member after the error, so "after" is never
	// passed to Frame for her.
	assertFrameCalls(t, logic.framedCallsFor("alice"), []string{"snapshot:alice", "x", "boom"}, []uint64{1, 2, 3})
}

func TestFramedQueueOverflowEndsMemberAndAdmitsNothingFurther(t *testing.T) {
	logic := newFramingLogic()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{PeerQueue: 1})

	slow := &fakePeer{block: make(chan struct{}), entered: make(chan struct{}, 1)} // never released: Send stays pending
	fast := &fakePeer{}
	mustJoin(t, hub, "room-1", "slow", slow)
	mustJoin(t, hub, "room-1", "fast", fast)
	waitForMessages(t, fast, 1)

	// slow's writer must have dequeued the snapshot and entered Send before
	// the queue slot is measured. Otherwise the snapshot may still occupy the
	// single slot and the first payload would overflow instead of the second.
	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow's writer never entered Send for the snapshot")
	}
	dispatch(t, hub, "room-1", "fast", "x")
	waitForMessages(t, fast, 2)
	dispatch(t, hub, "room-1", "fast", "y")
	waitFor(t, slow.isClosed, "slow was not closed after queue overflow")
	dispatch(t, hub, "room-1", "fast", "z")

	assertMessages(t, waitForMessages(t, fast, 4), []string{"1:snapshot:fast", expectedFrame(0, "x"), expectedFrame(1, "y"), expectedFrame(2, "z")})
	// The overflowed payload was offered to Frame with the candidate sequence
	// 3 and never admitted; the member ended, so nothing after it reaches Frame.
	assertFrameCalls(t, logic.framedCallsFor("slow"), []string{"snapshot:slow", "x", "y"}, []uint64{1, 2, 3})
}

func TestFramedBytesAreIsolatedPerMemberAndFromTheCaller(t *testing.T) {
	logic := newFramingLogic()
	logic.encode = func(to Principal, _ uint64, payload []byte) ([]byte, error) {
		if to.UserID == "alice" {
			// Overwrite the copy in place and return it: must not reach bob.
			for i := range payload {
				payload[i] = 'A'
			}
		}
		return payload, nil
	}
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind != EventAction {
			return nil
		}
		buf := []byte("hello")
		if err := rc.Broadcast(buf); err != nil {
			return err
		}
		// The caller mutates its slice after the send returned.
		buf[0] = 'X'
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	alice := &fakePeer{}
	bob := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice)
	mustJoin(t, hub, "room-1", "bob", bob)
	dispatch(t, hub, "room-1", "bob", "go")

	// The snapshot is framed too, so alice's copy of it is overwritten as well.
	assertMessages(t, waitForMessages(t, alice, 2), []string{strings.Repeat("A", len("snapshot:alice")), "AAAAA"})
	assertMessages(t, waitForMessages(t, bob, 2), []string{"snapshot:bob", "hello"})
}

func TestFramedBlockedSendDoesNotStopFrameForOtherMembers(t *testing.T) {
	logic := newFramingLogic()
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.Broadcast(ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{PeerQueue: 8})

	stalled := &fakePeer{block: make(chan struct{})} // Send never returns in this test
	fast := &fakePeer{}
	mustJoin(t, hub, "room-1", "stalled", stalled)
	mustJoin(t, hub, "room-1", "fast", fast)

	dispatch(t, hub, "room-1", "fast", "m1")
	dispatch(t, hub, "room-1", "fast", "m2")
	dispatch(t, hub, "room-1", "fast", "m3")

	// Frame calls are made on the room goroutine, before each dispatch
	// returns, so every call is complete while stalled's writer is still blocked.
	if got := logic.frameCallCount(); got != 8 {
		t.Fatalf("Frame calls while a peer is stalled = %d, want 8", got)
	}
	assertMessages(t, waitForMessages(t, fast, 4), []string{"1:snapshot:fast", expectedFrame(0, "m1"), expectedFrame(1, "m2"), expectedFrame(2, "m3")})
	if n := len(stalled.messages()); n != 0 {
		t.Fatalf("stalled peer received %d messages while blocked, want 0", n)
	}
}

func TestUnframedLogicDeliversRawPayloadsUnchanged(t *testing.T) {
	logic := &recordingLogic{}
	logic.handle = func(rc *RoomContext, ev Event) error {
		if ev.Kind == EventAction {
			return rc.BroadcastExcept("alice", ev.Payload)
		}
		return nil
	}
	hub, _ := newTestHub(t, func(string) Logic { return logic }, Options{})

	alice := &fakePeer{}
	bob := &fakePeer{}
	mustJoin(t, hub, "room-1", "alice", alice)
	mustJoin(t, hub, "room-1", "bob", bob)
	dispatch(t, hub, "room-1", "alice", "one")
	dispatch(t, hub, "room-1", "alice", "two")

	assertMessages(t, waitForMessages(t, bob, 3), []string{"snapshot:bob", "one", "two"})
	assertMessages(t, waitForMessages(t, alice, 1), []string{"snapshot:alice"})
}

func assertFrameCalls(t *testing.T, got []frameCall, payloads []string, seqs []uint64) {
	t.Helper()
	if len(got) != len(payloads) {
		t.Fatalf("Frame calls = %d, want %d: %+v", len(got), len(payloads), got)
	}
	for i, c := range got {
		if string(c.payload) != payloads[i] || c.seq != seqs[i] {
			t.Fatalf("Frame call %d = (seq %d, %q), want (seq %d, %q)", i, c.seq, c.payload, seqs[i], payloads[i])
		}
	}
}

func mustJoin(t *testing.T, hub *Hub, roomID, userID string, peer Peer) {
	t.Helper()
	if err := hub.Join(context.Background(), roomID, Principal{UserID: userID}, peer); err != nil {
		t.Fatalf("%s join %s: %v", userID, roomID, err)
	}
}

func dispatch(t *testing.T, hub *Hub, roomID, userID, payload string) {
	t.Helper()
	ev := Event{Kind: EventAction, RoomID: roomID, Principal: Principal{UserID: userID}, Payload: []byte(payload)}
	if err := hub.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("dispatch %s in %s: %v", payload, roomID, err)
	}
}

func assertMessages(t *testing.T, got [][]byte, want []string) {
	t.Helper()
	gotStrings := make([]string, len(got))
	for i, m := range got {
		gotStrings[i] = string(m)
	}
	if strings.Join(gotStrings, "|") != strings.Join(want, "|") {
		t.Fatalf("messages = %q, want %q", gotStrings, want)
	}
}

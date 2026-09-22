package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ws "github.com/coder/websocket"

	"github.com/angvp/tango"
	"github.com/angvp/tango/realtime"
)

// fakeCoordinator is a minimal, in-memory realtime.Coordinator stand-in for
// adapter-level tests that shouldn't depend on real room-loop timing.
type fakeCoordinator struct {
	mu            sync.Mutex
	joinCalls     int
	leaveCalls    int
	leaveArgs     []leaveCall
	dispatches    []realtime.Event
	dispatchPeers []realtime.Peer

	joinDelay chan struct{} // if non-nil, Join blocks on it before returning
	joinErr   error
}

type leaveCall struct {
	roomID    string
	principal realtime.Principal
	peer      realtime.Peer
}

func (f *fakeCoordinator) Join(ctx context.Context, roomID string, p realtime.Principal, peer realtime.Peer) error {
	if f.joinDelay != nil {
		select {
		case <-f.joinDelay:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.joinCalls++
	return f.joinErr
}

func (f *fakeCoordinator) Leave(ctx context.Context, roomID string, p realtime.Principal, peer realtime.Peer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaveCalls++
	f.leaveArgs = append(f.leaveArgs, leaveCall{roomID: roomID, principal: p, peer: peer})
	return nil
}

func (f *fakeCoordinator) DispatchPeer(ctx context.Context, ev realtime.Event, peer realtime.Peer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatches = append(f.dispatches, ev)
	f.dispatchPeers = append(f.dispatchPeers, peer)
	return nil
}

func (f *fakeCoordinator) dispatchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dispatches)
}

func (f *fakeCoordinator) leaveCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leaveCalls
}

func alwaysAuth(userID string) Authenticate {
	return func(*http.Request) (realtime.Principal, error) {
		return realtime.Principal{UserID: userID}, nil
	}
}

func fixedRoom(id string) func(*tango.Context) (string, error) {
	return func(*tango.Context) (string, error) { return id, nil }
}

func toWSURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func waitForCondition(t *testing.T, cond func() bool, msg string) {
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

func buildHandler(t *testing.T, coordinator realtime.Coordinator, authenticate Authenticate, roomID func(*tango.Context) (string, error)) http.Handler {
	t.Helper()
	view := View(coordinator, authenticate, roomID)
	app := tango.NewApp("ws-test", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/ws", view),
			tango.Path(http.MethodGet, "/rooms/{id}/ws", view),
		})
	})
	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{app}})
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("compile routes: %v", err)
	}
	return handler
}

func TestAuthenticationFailureNeverUpgrades(t *testing.T) {
	fc := &fakeCoordinator{}
	deniedAuth := func(*http.Request) (realtime.Principal, error) {
		return realtime.Principal{}, fmt.Errorf("denied")
	}
	server := httptest.NewServer(buildHandler(t, fc, deniedAuth, fixedRoom("room-1")))
	defer server.Close()

	resp, err := http.Get(server.URL + "/ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json (never an upgrade response)", got)
	}
	if fc.joinCalls != 0 {
		t.Fatalf("Join should never have been called, got %d calls", fc.joinCalls)
	}
}

func TestJoinBeforeReadOrdering(t *testing.T) {
	joinRelease := make(chan struct{})
	fc := &fakeCoordinator{joinDelay: joinRelease}
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), ws.MessageBinary, []byte("early")); err != nil {
		t.Fatalf("write before join completes: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // give the server ample time to have mishandled this, if it were going to

	if fc.dispatchCount() != 0 {
		t.Fatal("message sent before Join completed must not have been dispatched yet")
	}

	close(joinRelease)

	waitForCondition(t, func() bool { return fc.dispatchCount() == 1 }, "expected the early message to be dispatched once Join completed")
}

// TestReadLoopDispatchesThroughDispatchPeerWithItsOwnPeer proves View wires
// itself to Coordinator.DispatchPeer, carrying the exact same connPeer it
// registered via Join — never the unrestricted Coordinator.Dispatch, which
// realtime.Coordinator no longer even exposes to adapters.
func TestReadLoopDispatchesThroughDispatchPeerWithItsOwnPeer(t *testing.T) {
	fc := &fakeCoordinator{}
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), ws.MessageBinary, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForCondition(t, func() bool { return fc.dispatchCount() == 1 }, "expected the message to reach DispatchPeer")

	fc.mu.Lock()
	peer := fc.dispatchPeers[0]
	fc.mu.Unlock()
	if peer == nil {
		t.Fatal("DispatchPeer was called with a nil peer")
	}
	if _, ok := peer.(*connPeer); !ok {
		t.Fatalf("DispatchPeer peer type = %T, want *connPeer", peer)
	}
}

func TestBadRoomIDNeverUpgrades(t *testing.T) {
	fc := &fakeCoordinator{}
	failingRoomID := func(*tango.Context) (string, error) { return "", fmt.Errorf("no room") }
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), failingRoomID))
	defer server.Close()

	resp, err := http.Get(server.URL + "/ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if fc.joinCalls != 0 {
		t.Fatalf("Join should never have been called, got %d calls", fc.joinCalls)
	}
}

func TestCoordinatorJoinFailureClosesConnectionWithoutDispatch(t *testing.T) {
	fc := &fakeCoordinator{joinErr: fmt.Errorf("room is full")}
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("expected the connection to be closed after a Join failure")
	}
	if fc.dispatchCount() != 0 {
		t.Fatalf("no message should ever have been dispatched, got %d dispatches", fc.dispatchCount())
	}
}

// TestJoinFailureBeforeMembershipCallsLeaveExactlyOnce covers the
// pre-membership half of the "Join has no rollback" cleanup requirement: a
// Join failure that happens before any membership exists (e.g. a Snapshot
// error) must still make View call Leave — a safe, idempotent no-op in
// this case, but View has no way to know that from the error alone, so it
// always calls it — exactly once, with the same room/principal/peer Join
// was given.
func TestJoinFailureBeforeMembershipCallsLeaveExactlyOnce(t *testing.T) {
	fc := &fakeCoordinator{joinErr: fmt.Errorf("snapshot failed")}
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("expected the connection to be closed after a Join failure")
	}

	waitForCondition(t, func() bool { return fc.leaveCallCount() == 1 }, "expected exactly one best-effort Leave after a pre-membership Join failure")
	time.Sleep(50 * time.Millisecond)

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if got := len(fc.leaveArgs); got != 1 {
		t.Fatalf("Leave called %d times, want exactly 1", got)
	}
	got := fc.leaveArgs[0]
	if got.roomID != "room-1" || got.principal.UserID != "alice" {
		t.Fatalf("Leave args = %+v, want room-1/alice", got)
	}
}

// joinFailingLogic rejects every EventJoin, after membership and the
// snapshot delivery have already been applied by the room loop — exactly
// the "Join has no rollback" case View's best-effort Leave exists for.
type joinFailingLogic struct{}

func (joinFailingLogic) Handle(_ *realtime.RoomContext, ev realtime.Event) error {
	if ev.Kind == realtime.EventJoin {
		return fmt.Errorf("join rejected by logic")
	}
	return nil
}

func (joinFailingLogic) Snapshot(*realtime.RoomContext, realtime.Principal) ([]byte, error) {
	return []byte("snapshot"), nil
}

// TestJoinFailureAfterMembershipLeavesNoGhostPeer covers the
// post-membership half, against a real Hub: an EventJoin handler error
// leaves membership installed (Join has no rollback), so View's
// best-effort Leave is what actually removes the peer it just installed.
// If it didn't, the room would never go empty and could never evict.
func TestJoinFailureAfterMembershipLeavesNoGhostPeer(t *testing.T) {
	hub, err := realtime.NewHub(func(string) realtime.Logic { return joinFailingLogic{} }, realtime.Options{ReconnectWindow: 30 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	defer func() { _ = hub.Close(context.Background()) }()

	server := httptest.NewServer(buildHandler(t, hub, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// The snapshot delivery is asynchronous (its own outbound queue, drained
	// by a separate writer goroutine) and races the EventJoin failure that
	// then triggers the close — the client may legitimately observe the
	// snapshot before the close frame arrives. Drain until the connection
	// actually closes, with an overall deadline so a real regression still
	// fails the test instead of hanging.
	readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var closeErr error
	for {
		if _, _, err := conn.Read(readCtx); err != nil {
			closeErr = err
			break
		}
	}
	if closeErr == nil {
		t.Fatal("expected the connection to be closed after a post-membership Join failure")
	}

	waitForCondition(t, func() bool {
		return errors.Is(hub.Dispatch(context.Background(), realtime.Event{Kind: realtime.EventAction, RoomID: "room-1"}), realtime.ErrRoomNotFound)
	}, "room never evicted — the ghost peer installed before the EventJoin failure was never removed")
}

func TestOversizedMessageNeverDispatchedAndClosesConnection(t *testing.T) {
	fc := &fakeCoordinator{}
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	oversized := make([]byte, DefaultMaxMessageSize+1024)
	// The client-side write may itself succeed or fail depending on framing
	// timing; what matters is the server-side outcome, checked below.
	_ = conn.Write(context.Background(), ws.MessageBinary, oversized)

	waitForCondition(t, func() bool { return fc.leaveCallCount() == 1 }, "expected exactly one Leave after an oversized message closes the connection")
	if fc.dispatchCount() != 0 {
		t.Fatalf("oversized message should never have been dispatched, got %d dispatches", fc.dispatchCount())
	}
}

func TestLeaveCalledExactlyOnceOnClientInitiatedClose(t *testing.T) {
	fc := &fakeCoordinator{}
	server := httptest.NewServer(buildHandler(t, fc, alwaysAuth("alice"), fixedRoom("room-1")))
	defer server.Close()

	conn, _, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close(ws.StatusNormalClosure, "bye")

	waitForCondition(t, func() bool { return fc.leaveCallCount() == 1 }, "expected exactly one Leave after client-initiated close")
	time.Sleep(50 * time.Millisecond)
	if got := fc.leaveCallCount(); got != 1 {
		t.Fatalf("Leave called %d times, want exactly 1", got)
	}
}

// e2eLogic is a minimal realtime.Logic used only by the end-to-end test
// below: it echoes a fixed snapshot and broadcasts every action to every
// other member of the room.
type e2eLogic struct{}

func (e2eLogic) Handle(rc *realtime.RoomContext, ev realtime.Event) error {
	if ev.Kind == realtime.EventAction {
		return rc.BroadcastExcept(ev.Principal.UserID, ev.Payload)
	}
	return nil
}

func (e2eLogic) Snapshot(*realtime.RoomContext, realtime.Principal) ([]byte, error) {
	return []byte("snapshot"), nil
}

func TestEndToEndBroadcastAndReconnect(t *testing.T) {
	hub, err := realtime.NewHub(func(string) realtime.Logic { return e2eLogic{} }, realtime.Options{})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	defer func() { _ = hub.Close(context.Background()) }()

	authenticate := func(r *http.Request) (realtime.Principal, error) {
		userID := r.URL.Query().Get("user")
		if userID == "" {
			return realtime.Principal{}, fmt.Errorf("missing user")
		}
		return realtime.Principal{UserID: userID}, nil
	}
	roomID := func(ctx *tango.Context) (string, error) { return ctx.Param("id"), nil }

	server := httptest.NewServer(buildHandler(t, hub, authenticate, roomID))
	defer server.Close()
	base := toWSURL(server.URL)

	alice, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?user=alice", nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	defer alice.CloseNow()
	if _, _, err := alice.Read(context.Background()); err != nil {
		t.Fatalf("alice snapshot read: %v", err)
	}

	bob, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?user=bob", nil)
	if err != nil {
		t.Fatalf("bob dial: %v", err)
	}
	defer bob.CloseNow()
	if _, _, err := bob.Read(context.Background()); err != nil {
		t.Fatalf("bob snapshot read: %v", err)
	}

	if err := alice.Write(context.Background(), ws.MessageBinary, []byte("hi bob")); err != nil {
		t.Fatalf("alice write: %v", err)
	}
	_, got, err := bob.Read(context.Background())
	if err != nil {
		t.Fatalf("bob read broadcast: %v", err)
	}
	if string(got) != "hi bob" {
		t.Fatalf("bob received %q, want %q", got, "hi bob")
	}

	// Alice disconnects and reconnects within the reconnect window: she
	// should be restored to membership and receive a fresh snapshot.
	_ = alice.Close(ws.StatusNormalClosure, "")

	alice2, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?user=alice", nil)
	if err != nil {
		t.Fatalf("alice reconnect dial: %v", err)
	}
	defer alice2.CloseNow()
	_, snap, err := alice2.Read(context.Background())
	if err != nil {
		t.Fatalf("alice reconnect snapshot read: %v", err)
	}
	if string(snap) != "snapshot" {
		t.Fatalf("reconnect snapshot = %q, want %q", snap, "snapshot")
	}
}

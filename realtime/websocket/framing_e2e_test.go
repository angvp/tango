package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	ws "github.com/coder/websocket"

	"github.com/angvp/tango"
	"github.com/angvp/tango/realtime"
)

// framingReadTimeout bounds every read and write in these tests. Reads wait
// on observed messages; the timeout only turns a missing message into a
// failure instead of a hang.
const framingReadTimeout = 5 * time.Second

// framingE2EBase is the application behavior shared by the framed and plain
// logics. A client message is "<op>:<text>", and the op selects the outbound
// path under test. The whole client message is the payload, so the bytes a
// client reads show which path produced them.
type framingE2EBase struct{}

func (framingE2EBase) Handle(rc *realtime.RoomContext, ev realtime.Event) error {
	if ev.Kind != realtime.EventAction {
		return nil
	}
	op, _, _ := strings.Cut(string(ev.Payload), ":")
	switch op {
	case "broadcast":
		return rc.Broadcast(ev.Payload)
	case "except":
		return rc.BroadcastExcept(ev.Principal.UserID, ev.Payload)
	case "self":
		return rc.SendUser(ev.Principal.UserID, ev.Payload)
	}
	return nil
}

func (framingE2EBase) Snapshot(_ *realtime.RoomContext, p realtime.Principal) ([]byte, error) {
	return []byte("snapshot:" + p.UserID), nil
}

// framedE2ELogic opts its room in to framing. The host-chosen frame is
// "<seq>:<payload>".
type framedE2ELogic struct {
	framingE2EBase
}

func (framedE2ELogic) Frame(_ realtime.Principal, seq uint64, payload []byte) ([]byte, error) {
	return []byte(strconv.FormatUint(seq, 10) + ":" + string(payload)), nil
}

// plainE2ELogic does not implement realtime.Framer, so its room must produce
// exactly today's raw bytes.
type plainE2ELogic struct {
	framingE2EBase
}

func framingAuth(r *http.Request) (realtime.Principal, error) {
	userID := r.URL.Query().Get("user")
	return realtime.Principal{UserID: userID}, nil
}

func framingRoomID(ctx *tango.Context) (string, error) {
	return ctx.Param("id"), nil
}

// newFramingServer starts a real HTTP and WebSocket server over a Hub built
// from factory. It returns the ws:// base URL and the Hub the test holds to
// call Hub.SendUser directly.
func newFramingServer(t *testing.T, factory realtime.Factory) (string, *realtime.Hub) {
	t.Helper()
	hub, err := realtime.NewHub(factory, realtime.Options{})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close(context.Background()) })

	server := httptest.NewServer(buildHandler(t, hub, framingAuth, framingRoomID))
	t.Cleanup(server.Close)
	return toWSURL(server.URL), hub
}

// dialRoom opens a real WebSocket as user into room.
func dialRoom(t *testing.T, base, room, user string) *ws.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), framingReadTimeout)
	defer cancel()
	conn, _, err := ws.Dial(ctx, base+"/rooms/"+room+"/ws?user="+user, nil)
	if err != nil {
		t.Fatalf("dial %s as %s: %v", room, user, err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func writeMessage(t *testing.T, conn *ws.Conn, msg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), framingReadTimeout)
	defer cancel()
	if err := conn.Write(ctx, ws.MessageBinary, []byte(msg)); err != nil {
		t.Fatalf("write %q: %v", msg, err)
	}
}

// expectMessages reads exactly len(want) messages from conn, in order, and
// fails on the first mismatch.
func expectMessages(t *testing.T, conn *ws.Conn, want ...string) {
	t.Helper()
	for i, w := range want {
		ctx, cancel := context.WithTimeout(context.Background(), framingReadTimeout)
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("message %d: read: %v (want %q)", i, err, w)
		}
		if got := string(data); got != w {
			t.Fatalf("message %d = %q, want %q", i, got, w)
		}
	}
}

// TestFramingE2ESequenceAcrossOutboundPaths drives all four outbound paths
// through a real socket: a join snapshot, Broadcast, BroadcastExcept, room
// SendUser, and Hub.SendUser. Alice's stream must be 1 through 5 with no gaps.
func TestFramingE2ESequenceAcrossOutboundPaths(t *testing.T) {
	base, hub := newFramingServer(t, func(string) realtime.Logic { return framedE2ELogic{} })

	alice := dialRoom(t, base, "r1", "alice")
	bob := dialRoom(t, base, "r1", "bob")
	expectMessages(t, alice, "1:snapshot:alice")
	expectMessages(t, bob, "1:snapshot:bob")

	writeMessage(t, alice, "broadcast:one")
	expectMessages(t, alice, "2:broadcast:one")
	expectMessages(t, bob, "2:broadcast:one")

	writeMessage(t, bob, "except:two")
	expectMessages(t, alice, "3:except:two")

	writeMessage(t, alice, "self:three")
	expectMessages(t, alice, "4:self:three")

	ctx, cancel := context.WithTimeout(context.Background(), framingReadTimeout)
	defer cancel()
	if err := hub.SendUser(ctx, "alice", []byte("hub:five")); err != nil {
		t.Fatalf("Hub.SendUser: %v", err)
	}
	expectMessages(t, alice, "5:hub:five")
}

// TestFramingE2EIndependentSequencesPerConnectionAndRoom checks that two
// clients in one room and clients in a second room each keep their own
// counter, each starting from their own snapshot.
func TestFramingE2EIndependentSequencesPerConnectionAndRoom(t *testing.T) {
	base, _ := newFramingServer(t, func(string) realtime.Logic { return framedE2ELogic{} })

	alice := dialRoom(t, base, "r1", "alice")
	bob := dialRoom(t, base, "r1", "bob")
	carol := dialRoom(t, base, "r2", "carol")
	dave := dialRoom(t, base, "r2", "dave")
	expectMessages(t, alice, "1:snapshot:alice")
	expectMessages(t, bob, "1:snapshot:bob")
	expectMessages(t, carol, "1:snapshot:carol")
	expectMessages(t, dave, "1:snapshot:dave")

	writeMessage(t, alice, "broadcast:a")
	expectMessages(t, alice, "2:broadcast:a")
	expectMessages(t, bob, "2:broadcast:a")

	writeMessage(t, bob, "broadcast:b")
	expectMessages(t, alice, "3:broadcast:b")
	expectMessages(t, bob, "3:broadcast:b")

	writeMessage(t, carol, "broadcast:c")
	expectMessages(t, carol, "2:broadcast:c")
	expectMessages(t, dave, "2:broadcast:c")

	// Room r2's frame must not leak into r1. The next r1 frame is 4, and
	// it arrives right after a 3, so a leaked frame would show up first.
	writeMessage(t, alice, "broadcast:d")
	expectMessages(t, alice, "4:broadcast:d")
	expectMessages(t, bob, "4:broadcast:d")
}

// TestFramingE2EReconnectRestartsAtOne covers a replacement that restarts
// the counter, and a replaced socket that receives no further frame.
func TestFramingE2EReconnectRestartsAtOne(t *testing.T) {
	t.Run("live replacement closes the old socket with no further frames", func(t *testing.T) {
		base, _ := newFramingServer(t, func(string) realtime.Logic { return framedE2ELogic{} })

		alice1 := dialRoom(t, base, "r1", "alice")
		bob := dialRoom(t, base, "r1", "bob")
		expectMessages(t, alice1, "1:snapshot:alice")
		expectMessages(t, bob, "1:snapshot:bob")

		writeMessage(t, alice1, "broadcast:a")
		expectMessages(t, alice1, "2:broadcast:a")
		expectMessages(t, bob, "2:broadcast:a")

		alice2 := dialRoom(t, base, "r1", "alice")
		expectMessages(t, alice2, "1:snapshot:alice")

		writeMessage(t, alice2, "broadcast:b")
		expectMessages(t, alice2, "2:broadcast:b")
		expectMessages(t, bob, "3:broadcast:b")

		// The replaced socket is closed by the server. Its first read must
		// be that close, not a data frame sent after the replacement.
		ctx, cancel := context.WithTimeout(context.Background(), framingReadTimeout)
		defer cancel()
		if _, data, err := alice1.Read(ctx); err == nil {
			t.Fatalf("replaced socket received %q, want it closed with no further frames", data)
		}
	})

	t.Run("close then dial starts a fresh snapshot at one", func(t *testing.T) {
		base, _ := newFramingServer(t, func(string) realtime.Logic { return framedE2ELogic{} })

		alice1 := dialRoom(t, base, "r1", "alice")
		bob := dialRoom(t, base, "r1", "bob")
		expectMessages(t, alice1, "1:snapshot:alice")
		expectMessages(t, bob, "1:snapshot:bob")

		writeMessage(t, alice1, "broadcast:a")
		expectMessages(t, alice1, "2:broadcast:a")
		expectMessages(t, bob, "2:broadcast:a")

		if err := alice1.Close(ws.StatusNormalClosure, ""); err != nil {
			t.Fatalf("client close: %v", err)
		}

		alice2 := dialRoom(t, base, "r1", "alice")
		expectMessages(t, alice2, "1:snapshot:alice")

		writeMessage(t, alice2, "broadcast:b")
		expectMessages(t, alice2, "2:broadcast:b")
		expectMessages(t, bob, "3:broadcast:b")
	})
}

// TestFramingE2EMixedRoomsFrameOnlyOptedInRoom runs an opted-in room and a
// non-opted room on one Hub. Framed bytes appear only in the opted-in room;
// the other room's bytes are the raw payloads, byte for byte.
func TestFramingE2EMixedRoomsFrameOnlyOptedInRoom(t *testing.T) {
	factory := func(roomID string) realtime.Logic {
		if roomID == "framed" {
			return framedE2ELogic{}
		}
		return plainE2ELogic{}
	}
	base, hub := newFramingServer(t, factory)

	alice := dialRoom(t, base, "framed", "alice")
	carol := dialRoom(t, base, "plain", "carol")
	expectMessages(t, alice, "1:snapshot:alice")
	expectMessages(t, carol, "snapshot:carol")

	writeMessage(t, alice, "broadcast:a")
	expectMessages(t, alice, "2:broadcast:a")

	writeMessage(t, carol, "broadcast:b")
	expectMessages(t, carol, "broadcast:b")

	ctx, cancel := context.WithTimeout(context.Background(), framingReadTimeout)
	defer cancel()
	if err := hub.SendUser(ctx, "carol", []byte("hub:c")); err != nil {
		t.Fatalf("Hub.SendUser carol: %v", err)
	}
	expectMessages(t, carol, "hub:c")

	if err := hub.SendUser(ctx, "alice", []byte("hub:a")); err != nil {
		t.Fatalf("Hub.SendUser alice: %v", err)
	}
	expectMessages(t, alice, "3:hub:a")
}

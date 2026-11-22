package main

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	ws "github.com/coder/websocket"
)

func toWSURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func readWithTimeout(t *testing.T, conn *ws.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return payload
}

// frame is one received message split into its sequence and its text.
type frame struct {
	seq  uint64
	text string
}

// readFrame reads one message and splits the sequence line the room's Frame
// wrote from the payload.
func readFrame(t *testing.T, conn *ws.Conn) frame {
	t.Helper()
	raw := string(readWithTimeout(t, conn))
	head, text, ok := strings.Cut(raw, "\n")
	if !ok {
		t.Fatalf("message %q has no sequence line", raw)
	}
	seq, err := strconv.ParseUint(head, 10, 64)
	if err != nil {
		t.Fatalf("message %q: bad sequence %q: %v", raw, head, err)
	}
	return frame{seq: seq, text: text}
}

func TestChatBroadcastIdleTimerAndReconnect(t *testing.T) {
	jwtService, err := newJWTService()
	if err != nil {
		t.Fatalf("newJWTService: %v", err)
	}
	hub, err := newChatHub()
	if err != nil {
		t.Fatalf("newChatHub: %v", err)
	}
	defer func() { _ = hub.Close(context.Background()) }()

	handler, err := buildHandler(hub, jwtService)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	base := toWSURL(server.URL)

	aliceToken, err := jwtService.Issue("alice", time.Minute)
	if err != nil {
		t.Fatalf("issue alice token: %v", err)
	}
	bobToken, err := jwtService.Issue("bob", time.Minute)
	if err != nil {
		t.Fatalf("issue bob token: %v", err)
	}

	alice, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?token="+aliceToken, nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	defer alice.CloseNow()
	if got := readFrame(t, alice); got != (frame{1, "welcome alice"}) {
		t.Fatalf("alice snapshot = %+v, want sequence 1", got)
	}

	bob, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?token="+bobToken, nil)
	if err != nil {
		t.Fatalf("bob dial: %v", err)
	}
	defer bob.CloseNow()
	if got := readFrame(t, bob); got != (frame{1, "welcome bob"}) {
		t.Fatalf("bob snapshot = %+v, want sequence 1", got)
	}

	// Alice observes bob's join notice next.
	if got := readFrame(t, alice); got != (frame{2, "bob joined"}) {
		t.Fatalf("alice join notice = %+v", got)
	}

	if err := alice.Write(context.Background(), ws.MessageBinary, []byte("hi bob")); err != nil {
		t.Fatalf("alice write: %v", err)
	}
	if got := readFrame(t, bob); got != (frame{2, "alice: hi bob"}) {
		t.Fatalf("bob received = %+v", got)
	}

	// Alice's message reset her idle timer; nobody else has one running, so
	// the next broadcast either of them sees is her idle notice.
	if got := readFrame(t, bob); got != (frame{3, "alice is idle"}) {
		t.Fatalf("bob idle notice = %+v", got)
	}
	if got := readFrame(t, alice); got != (frame{3, "alice is idle"}) {
		t.Fatalf("alice idle notice (Broadcast includes the sender) = %+v", got)
	}

	// Alice disconnects and reconnects within the reconnect window: she is
	// restored to membership and gets a fresh Snapshot, framed as sequence 1
	// again: the sequence restarts on every connection.
	_ = alice.Close(ws.StatusNormalClosure, "")

	alice2, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?token="+aliceToken, nil)
	if err != nil {
		t.Fatalf("alice reconnect dial: %v", err)
	}
	defer alice2.CloseNow()
	if got := readFrame(t, alice2); got != (frame{1, "welcome alice"}) {
		t.Fatalf("alice reconnect snapshot = %+v, want sequence 1", got)
	}
}

func TestUnauthenticatedConnectionIsRejected(t *testing.T) {
	jwtService, err := newJWTService()
	if err != nil {
		t.Fatalf("newJWTService: %v", err)
	}
	hub, err := newChatHub()
	if err != nil {
		t.Fatalf("newChatHub: %v", err)
	}
	defer func() { _ = hub.Close(context.Background()) }()

	handler, err := buildHandler(hub, jwtService)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	_, resp, err := ws.Dial(context.Background(), toWSURL(server.URL)+"/rooms/r1/ws", nil)
	if err == nil {
		t.Fatal("expected dial without a token to fail")
	}
	if resp == nil || resp.StatusCode != 401 {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("status = %d, want 401", status)
	}
}

package main

import (
	"context"
	"net/http/httptest"
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
	if got := string(readWithTimeout(t, alice)); got != "welcome alice" {
		t.Fatalf("alice snapshot = %q", got)
	}

	bob, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?token="+bobToken, nil)
	if err != nil {
		t.Fatalf("bob dial: %v", err)
	}
	defer bob.CloseNow()
	if got := string(readWithTimeout(t, bob)); got != "welcome bob" {
		t.Fatalf("bob snapshot = %q", got)
	}

	// Alice observes bob's join notice next.
	if got := string(readWithTimeout(t, alice)); got != "bob joined" {
		t.Fatalf("alice join notice = %q", got)
	}

	if err := alice.Write(context.Background(), ws.MessageBinary, []byte("hi bob")); err != nil {
		t.Fatalf("alice write: %v", err)
	}
	if got := string(readWithTimeout(t, bob)); got != "alice: hi bob" {
		t.Fatalf("bob received = %q", got)
	}

	// Alice's message reset her idle timer; nobody else has one running, so
	// the next broadcast either of them sees is her idle notice.
	if got := string(readWithTimeout(t, bob)); got != "alice is idle" {
		t.Fatalf("bob idle notice = %q", got)
	}
	if got := string(readWithTimeout(t, alice)); got != "alice is idle" {
		t.Fatalf("alice idle notice (Broadcast includes the sender) = %q", got)
	}

	// Alice disconnects and reconnects within the reconnect window: she is
	// restored to membership and gets a fresh Snapshot.
	_ = alice.Close(ws.StatusNormalClosure, "")

	alice2, _, err := ws.Dial(context.Background(), base+"/rooms/r1/ws?token="+aliceToken, nil)
	if err != nil {
		t.Fatalf("alice reconnect dial: %v", err)
	}
	defer alice2.CloseNow()
	if got := string(readWithTimeout(t, alice2)); got != "welcome alice" {
		t.Fatalf("alice reconnect snapshot = %q", got)
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

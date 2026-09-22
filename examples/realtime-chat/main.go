package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/angvp/tango"
	tangojwt "github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/realtime"
	realtimews "github.com/angvp/tango/realtime/websocket"
)

var exampleSecret = []byte("example-only-secret-not-for-production-use")

// idleTimeout is deliberately short (a real chat app would use minutes,
// not milliseconds) so this example's own integration test doesn't need
// to wait long for the idle-timer demonstration to fire.
const idleTimeout = 300 * time.Millisecond

func main() {
	os.Exit(run())
}

func run() int {
	issueSubject := flag.String("issue-token", "", "issue a development-only JWT for this subject and exit")
	check := flag.Bool("check", false, "validate app registration and exit")
	flag.Parse()

	jwtService, err := newJWTService()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *issueSubject != "" {
		token, err := jwtService.Issue(*issueSubject, 15*time.Minute)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(token)
		return 0
	}

	hub, err := newChatHub()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if *check {
		if err := tango.Check(exampleConfig(hub, jwtService)); err != nil {
			fmt.Fprintln(os.Stderr, "check failed:", err)
			return 1
		}
		fmt.Println("check passed")
		return 0
	}

	// signal.NotifyContext is the host's own responsibility — tanGO never
	// installs OS signal handling itself. Canceling this ctx (Ctrl-C or
	// SIGTERM) triggers ServeContext's graceful shutdown: draining
	// in-flight connections, then stopping hub.Close via the registered
	// "chat-hub" Lifecycle.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("listening on :8000")
	if err := tango.ServeContext(ctx, exampleConfig(hub, jwtService), nil, db.SQLite); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func newJWTService() (*tangojwt.Service, error) {
	return tangojwt.NewService(
		tangojwt.Key{ID: "example-v1", Secret: exampleSecret},
		nil,
		"realtime-chat-example",
		"realtime-chat-example",
	)
}

func newChatHub() (*realtime.Hub, error) {
	return realtime.NewHub(func(string) realtime.Logic { return chatLogic{} }, realtime.Options{})
}

// authenticate verifies a JWT carried as a query-string token — WebSocket
// handshakes are initiated by browser client code that cannot always set a
// custom Authorization header, which is exactly the case jwt.QueryToken
// exists for (see Milestone 26). Prefer jwt.BearerToken wherever the
// transport allows it; this example demonstrates the query-token path
// specifically because WebSocket is the concrete case that needs it.
func authenticate(jwtService *tangojwt.Service) realtimews.Authenticate {
	extract := tangojwt.QueryToken("token")
	return func(r *http.Request) (realtime.Principal, error) {
		token, err := extract(r)
		if err != nil {
			return realtime.Principal{}, err
		}
		claims, err := jwtService.Verify(token)
		if err != nil {
			return realtime.Principal{}, err
		}
		return realtime.Principal{UserID: claims.Subject}, nil
	}
}

func buildHandler(hub *realtime.Hub, jwtService *tangojwt.Service) (http.Handler, error) {
	registry, err := tango.BuildRegistry(exampleConfig(hub, jwtService))
	if err != nil {
		return nil, err
	}
	if err := registry.RunRegistration(); err != nil {
		return nil, err
	}
	return registry.Routes().Handler()
}

func exampleConfig(hub *realtime.Hub, jwtService *tangojwt.Service) tango.Config {
	roomID := func(ctx *tango.Context) (string, error) { return ctx.Param("id"), nil }
	view := realtimews.View(hub, authenticate(jwtService), roomID)

	app := tango.NewApp("realtime-chat", func(registry *tango.Registry) error {
		if err := registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/rooms/{id}/ws", view, tango.Name("chat-ws")),
		}); err != nil {
			return err
		}
		return registry.RegisterLifecycle(tango.Lifecycle{
			Name: "chat-hub",
			Stop: hub.Close,
		})
	})
	return tango.Config{InstalledApps: []tango.App{app}, Addr: ":8000"}
}

// chatLogic is the one room type this example's Hub serves: a plain
// broadcast chat room with join/leave notices and a per-user idle timer.
// It carries no state of its own — membership and delivery are entirely
// realtime.Hub's job.
type chatLogic struct{}

func (chatLogic) Handle(rc *realtime.RoomContext, ev realtime.Event) error {
	switch ev.Kind {
	case realtime.EventJoin:
		// EventJoin fires after membership is installed, so the joiner is
		// already in the room — BroadcastExcept keeps them from seeing
		// their own "joined" notice. EventLeave doesn't need the same
		// treatment: membership is removed before Handle runs for it, so
		// Broadcast already excludes the leaver naturally.
		return rc.BroadcastExcept(ev.Principal.UserID, []byte(ev.Principal.UserID+" joined"))
	case realtime.EventLeave:
		return rc.Broadcast([]byte(ev.Principal.UserID + " left"))
	case realtime.EventAction:
		if err := rc.ResetTimer("idle:"+ev.Principal.UserID, idleTimeout); err != nil {
			return err
		}
		return rc.BroadcastExcept(ev.Principal.UserID, append([]byte(ev.Principal.UserID+": "), ev.Payload...))
	case realtime.EventTimer:
		userID := strings.TrimPrefix(ev.Timer, "idle:")
		return rc.Broadcast([]byte(userID + " is idle"))
	}
	return nil
}

func (chatLogic) Snapshot(_ *realtime.RoomContext, p realtime.Principal) ([]byte, error) {
	return []byte("welcome " + p.UserID), nil
}

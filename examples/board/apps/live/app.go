// Package live pushes new posts to every open front page over a WebSocket.
package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/realtime"
	realtimews "github.com/angvp/tango/realtime/websocket"
)

// The whole board shares one room; every visitor watches the same feed.
const room = "feed"

// publisher is the server's own identity in the room. Only it may post.
var publisher = realtime.Principal{UserID: "board"}

// Feed announces new posts to connected browsers.
type Feed struct {
	hub *realtime.Hub
}

func NewFeed() (*Feed, error) {
	hub, err := realtime.NewHub(func(string) realtime.Logic { return feedLogic{} }, realtime.Options{})
	if err != nil {
		return nil, err
	}
	return &Feed{hub: hub}, nil
}

type message struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// Publish sends a new post to everyone watching. When nobody is connected
// the room doesn't exist yet, and there's nothing to do.
func (f *Feed) Publish(ctx context.Context, id int64, title string) error {
	payload, err := json.Marshal(message{Type: "post", ID: id, Title: title})
	if err != nil {
		return err
	}
	err = f.hub.Dispatch(ctx, realtime.Event{
		Kind:      realtime.EventAction,
		RoomID:    room,
		Principal: publisher,
		Payload:   payload,
	})
	if errors.Is(err, realtime.ErrRoomNotFound) {
		return nil
	}
	return err
}

// App mounts the WebSocket endpoint and closes the hub on shutdown.
func (f *Feed) App() tango.App {
	return tango.NewApp("live", func(registry *tango.Registry) error {
		view := realtimews.View(f.hub, visitor, func(*tango.Context) (string, error) {
			return room, nil
		})
		if err := registry.Routes().Include("/live/", tango.URLs{
			tango.Path("GET", "/ws/", view, tango.Name("ws")),
		}); err != nil {
			return err
		}
		return registry.RegisterLifecycle(tango.Lifecycle{Name: "live-feed", Stop: f.hub.Close})
	})
}

// visitor gives each connection its own anonymous identity: the feed is
// public, so there's nobody to authenticate.
func visitor(*http.Request) (realtime.Principal, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return realtime.Principal{}, err
	}
	return realtime.Principal{UserID: "visitor-" + hex.EncodeToString(id)}, nil
}

// feedLogic runs inside the room. It relays the publisher's messages to
// everyone and ignores whatever browsers send.
type feedLogic struct{}

func (feedLogic) Handle(room *realtime.RoomContext, ev realtime.Event) error {
	if ev.Kind != realtime.EventAction || ev.Principal != publisher {
		return nil
	}
	return room.Broadcast(ev.Payload)
}

func (feedLogic) Snapshot(*realtime.RoomContext, realtime.Principal) ([]byte, error) {
	return []byte(`{"type":"ready"}`), nil
}

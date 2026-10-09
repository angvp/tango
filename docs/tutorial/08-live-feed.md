# Tutorial, part 8: a live feed with WebSockets

Continuing from [part 7](07-api-tokens-and-rate-limits.md), this part makes the front page live: when anyone publishes a post — from the form or through the API — it appears at the top of every open front page within a moment, no reload needed.

tanGO's `realtime` package manages **rooms**: named groups of connections, each with a piece of your code (its `Logic`) that decides what happens when something arrives. `realtime/websocket` connects browsers to a room over a WebSocket. The board needs the simplest possible room: one shared feed, where the server talks and browsers listen.

## The `live` app

```sh
tango newapp live
```

Replace `apps/live/app.go` with:

```go
// apps/live/app.go
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
```

How the pieces fit:

- **`realtime.NewHub`** creates a hub, which runs rooms. Its argument builds the `Logic` for each room the first time someone joins it. A room exists as long as someone is connected, and disappears shortly after the last visitor leaves.
- **`realtimews.View`** is an ordinary view that upgrades the request to a WebSocket and joins the connection to a room. Its second argument decides *who* is connecting. The feed is public, so `visitor` just hands every connection a random identity. For a private room you'd check a session or a token here, and returning an error refuses the connection with a `401` before it ever opens.
- **`feedLogic`** is the room's `Logic`. Every message in a room — including anything a browser sends over its socket — arrives in `Handle`. Here only messages from `publisher`, the server's own identity, are broadcast; everything else is dropped, so a visitor can't push fake posts to everyone else. `Snapshot` is what each new connection receives first.
- **`Publish`** sends a message into the room as `publisher` with `hub.Dispatch`, which doesn't need a connection of its own. If nobody is watching, the room doesn't exist and `Dispatch` returns `realtime.ErrRoomNotFound` — nothing to do, so `Publish` treats that as success.
- **`RegisterLifecycle`** tells tanGO to call `hub.Close` when the app shuts down, so open sockets are closed cleanly. [Part 9](09-jobs-logging-and-shutdown.md) wires up the shutdown that triggers it.

Messages are whatever bytes you choose; the feed uses small JSON objects like `{"type":"post","id":3,"title":"Bike for sale"}`.

By default the WebSocket view only accepts connections from pages served by the same host, so another site can't open a socket to your feed from its own pages. If your front end lives on a different origin, allow it explicitly with `realtimews.WithOriginPatterns`.

## Wire it up

The feed is created in `project.Config` and handed to the apps that publish, the same way the store and token service are:

```go
// project/project.go (as of part 8)
feed, err := live.NewFeed()
if err != nil {
	log.Fatal(err)
}

config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
config.InstalledApps = []tango.App{
	accounts.New(store),
	posts.New(store, tokens, feed),
	api.New(store, tokens),
	web.New(store, feed),
	feed.App(),
	admin.New(store),
}
```

(import `"board/apps/live"`.) `posts.New` and `web.New` take the feed as a new parameter. In `web`, store it on `pages` (a `feed *live.Feed` field, set in `New` alongside `store`); in `posts`, pass it through to `createPost`, which gains a `feed *live.Feed` parameter. Each file that names `*live.Feed` imports `"board/apps/live"` too.

## Publish when a post is created

In both places a post is created — `createPost` in `apps/posts/views.go` and in `apps/web/views.go` — publish right after `store.Create` succeeds. The API's version:

```go
// apps/posts/views.go
if err := store.Create(ctx.Context(), meta, &post); err != nil {
	return err
}
if err := feed.Publish(ctx.Context(), post.ID, post.Title); err != nil {
	ctx.Logger().Warn("live feed publish failed", "post", post.ID, "err", err)
}
return ctx.JSON(http.StatusCreated, post)
```

and the form's, in `apps/web/views.go`:

```go
// apps/web/views.go
if err := p.store.Create(ctx.Context(), p.postMeta, &post); err != nil {
	return err
}
if err := p.feed.Publish(ctx.Context(), post.ID, post.Title); err != nil {
	ctx.Logger().Warn("live feed publish failed", "post", post.ID, "err", err)
}
```

A failed publish is logged, not returned: the post is saved either way, and a hiccup in the live feed shouldn't turn a successful post into an error page. `ctx.Logger()` is a standard `log/slog` logger that already knows which route and request it's logging for.

## Listen in the browser

The front page opens a socket and prepends each post it hears about. Replace `apps/web/templates/home.gohtml`:

```html
{{define "home"}}
{{template "header" .}}
<h1>Latest posts</h1>
<div id="posts" data-feed="{{url "live:ws"}}" data-post-link="{{url "post" "id" "{id}"}}">
{{range .Posts}}
  <article class="summary">
    <h2><a href="{{url "post" "id" .ID}}">{{.Title}}</a></h2>
    <p class="meta">{{.CreatedAt.Format "Jan 2, 2006"}} · {{.CommentCount}} comment{{if ne .CommentCount 1}}s{{end}}</p>
  </article>
{{else}}
  <p id="no-posts">No posts yet.</p>
{{end}}
</div>
<script>
  // Prepend posts as they're published, without reloading the page.
  (() => {
    const list = document.getElementById("posts");
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(scheme + "//" + location.host + list.dataset.feed);
    socket.binaryType = "arraybuffer"; // tanGO sends payloads as binary frames

    socket.addEventListener("message", (event) => {
      const msg = JSON.parse(new TextDecoder().decode(event.data));
      if (msg.type !== "post") return;

      // Build the entry with textContent, never innerHTML: titles come from users.
      const link = document.createElement("a");
      link.href = list.dataset.postLink.replace("{id}", msg.id);
      link.textContent = msg.title;
      const heading = document.createElement("h2");
      heading.append(link);
      const meta = document.createElement("p");
      meta.className = "meta";
      meta.textContent = "Just now · 0 comments";
      const article = document.createElement("article");
      article.className = "summary new";
      article.append(heading, meta);

      document.getElementById("no-posts")?.remove();
      list.prepend(article);
    });
  })();
</script>
{{template "footer" .}}
{{end}}
```

The URLs still come from route names: `{{url "live:ws"}}` for the socket, and `{{url "post" "id" "{id}"}}` renders the pattern `/p/{id}/`, which the script fills in. (The attribute is `data-post-link`, not `data-post-url`: `html/template` treats any attribute with `url` in its name as a URL and would percent-encode the braces.)

Two things in the script are easy to get wrong:

- **Messages arrive as binary frames.** tanGO sends payloads as bytes, which a browser hands you as a `Blob` by default. Setting `binaryType = "arraybuffer"` and decoding with `TextDecoder` gets you the JSON string back.
- **The new entry is built with `textContent`.** The server escaped titles when rendering the page, but this entry is created in the browser, and `innerHTML` with a title like `<img src=x onerror=...>` would run it. `textContent` always inserts plain text.

A short highlight for new arrivals, appended to `apps/web/static/board.css`:

```css
.new { animation: arrive 1.5s ease-out; }
@keyframes arrive { from { background: #e0f2fe; } to { background: transparent; } }
```

## Try it

```sh
go mod tidy
go run .
```

(`go mod tidy` fetches the WebSocket library `realtime/websocket` uses.) Open `http://localhost:8000/` in two browser windows side by side. Publish a post from one — the form or `curl` with a token from part 7 — and it slides in at the top of both, without a reload.

The feed lives in memory in one process: a visitor only hears about posts published while they're connected, and two copies of the app behind a load balancer would each have their own rooms. See [realtime rooms and WebSockets](../guides/realtime-websockets.md) for timers, reconnects, per-user messages, and the rest of the room API.

**Part 9** adds work that runs on a schedule, structured logs for every request, and a shutdown that finishes in-flight requests before the process exits.

Continue: [Tutorial, part 9: jobs, logging, and graceful shutdown](09-jobs-logging-and-shutdown.md)

// Package greetings is a reusable tanGO app: a real, importable Go package
// distributed as its own module (see the sibling go.mod at the repo root of
// this example), demonstrating every public registration surface a
// reusable app can use — a model, a namespaced route, an admin
// registration, and a check — with no host-specific code anywhere.
package greetings

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// staticFS embeds this app's own static assets. A reusable app owns and
// serves its own templates/static files this way — via an ordinary route,
// with no separate framework-level asset mechanism. See "App-owned
// templates and static assets" in docs/guides/reusable-apps.md.
//
//go:embed static
var staticFS embed.FS

// Greeting is the model this app contributes. Its name is deliberately
// specific (not "Message" or "Entry") to avoid colliding with a model a
// host project or another reusable app might register — see the naming
// discipline documented in docs/guides/reusable-apps.md.
type Greeting struct {
	ID        int64 `tango:"pk"`
	Name      string
	CreatedAt time.Time
}

// App is the greetings reusable app. It is a plain struct (rather than a
// tango.NewApp closure) purely so it can also implement Checker — the
// public App contract used is identical either way.
type App struct {
	store *db.Store
}

// New constructs the greetings reusable app. store is the host project's
// *db.Store — the only host-owned dependency this app needs; it never
// receives or defines the host's DB dialect/DSN itself.
func New(store *db.Store) tango.App {
	return App{store: store}
}

func (a App) Name() string {
	return "greetings"
}

func (a App) Register(registry *tango.Registry) error {
	if err := registry.Models().Register(Greeting{}); err != nil {
		return err
	}
	if err := registry.Admin().Register(Greeting{}, admin.Options{
		ListDisplay: []string{"Name", "CreatedAt"},
		Search:      []string{"Name"},
		Ordering:    []string{"CreatedAt"},
		// Widgets demonstrates a reusable app contributing its own
		// admin.Widget (Milestone 16) — no admin-internals access, no new
		// framework API, the same "ordinary Go import" story Milestone 13
		// established. See widget.go.
		Widgets: map[string]admin.Widget{"Name": NameWidget()},
	}); err != nil {
		return err
	}

	meta, _ := registry.Models().Get("Greeting")
	return registry.Routes().Include("/greetings/", tango.URLs{
		tango.Path("GET", "/", listGreetings(a.store, meta), tango.Name("list")),
		tango.Path("POST", "/", createGreeting(a.store, meta), tango.Name("create")),
		tango.Path("GET", "/static/*", serveStatic(), tango.Name("static")),
	})
}

func serveStatic() tango.View {
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/greetings/static/", http.FileServer(http.FS(assets)))
	return func(ctx *tango.Context) error {
		fileServer.ServeHTTP(ctx.ResponseWriter(), ctx.Request())
		return nil
	}
}

// Checks reports one advisory finding: whether the app's dependency on a
// non-nil store was satisfied. It demonstrates that a reusable app
// contributes checks through the same optional Checker capability any app
// uses — nothing host-specific.
func (a App) Checks() []tango.AppCheck {
	var err error
	if a.store == nil {
		err = errNilStore
	}
	return []tango.AppCheck{
		{Description: "greetings app has a store", Err: err},
	}
}

var errNilStore = errors.New("greetings: store must not be nil")

func listGreetings(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var greetings []Greeting
		if err := store.List(ctx.Context(), meta, db.Query{OrderBy: []string{"-CreatedAt"}}, &greetings); err != nil {
			return err
		}
		return ctx.JSON(200, greetings)
	}
}

func createGreeting(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var g Greeting
		if err := ctx.Bind(&g); err != nil {
			return err
		}
		if err := store.Create(ctx.Context(), meta, &g); err != nil {
			return err
		}
		return ctx.JSON(201, g)
	}
}

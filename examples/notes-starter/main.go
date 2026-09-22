package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/ratelimit"

	"notes-starter/migrations"

	_ "modernc.org/sqlite"
)

// createLimiter throttles note creation per client IP: 5 notes, refilling
// one every 10 seconds. A real app would tune this to its own traffic
// shape — see docs/guides/rate-limiting.md.
var createLimiter = mustNewCreateLimiter()

func mustNewCreateLimiter() *ratelimit.Limiter {
	limiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 5, Refill: 10 * time.Second})
	if err != nil {
		panic(err)
	}
	return limiter
}

type Note struct {
	ID      int64 `tango:"pk"`
	Title   string
	Body    string
	Private bool
}

type NotesApp struct {
	store *db.Store
	meta  model.ModelMeta
}

func (a *NotesApp) Name() string { return "notes" }

func (a *NotesApp) Register(registry *tango.Registry) error {
	if err := registry.Models().Register(Note{}); err != nil {
		return err
	}
	meta, ok := registry.Models().Get("Note")
	if !ok {
		return errors.New("Note model is not registered")
	}
	a.meta = meta
	if err := registry.Admin().Register(Note{}, admin.Options{
		ListDisplay: []string{"Title", "Private"},
		Search:      []string{"Title", "Body"},
		Ordering:    []string{"Title"},
	}); err != nil {
		return err
	}
	return registry.Routes().Include("/api/notes/", tango.URLs{
		tango.Path("GET", "/", a.list, tango.Name("list")),
		tango.Path("POST", "/", a.create, tango.Name("create"),
			tango.Use(ratelimit.Middleware(createLimiter, ratelimit.RemoteIPKey()))),
	})
}

func (a *NotesApp) list(ctx *tango.Context) error {
	var notes []Note
	if err := a.store.List(ctx.Context(), a.meta, db.Query{Limit: 50, OrderBy: []string{"Title"}}, &notes); err != nil {
		return err
	}
	return ctx.JSON(200, notes)
}

func (a *NotesApp) create(ctx *tango.Context) error {
	var note Note
	if err := ctx.Bind(&note); err != nil {
		return err
	}
	if err := a.store.Create(ctx.Context(), a.meta, &note); err != nil {
		return err
	}
	return ctx.JSON(201, note)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := tango.LoadEnvFile(".env"); err != nil {
		return err
	}
	if os.Getenv("TANGO_DB_DIALECT") == "" {
		os.Setenv("TANGO_DB_DIALECT", "sqlite")
	}

	dsn := tango.LoadDBDSNFromEnv()
	if dsn == "" {
		dsn = "app.db"
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, db.SQLite)
	config := appConfig(store)

	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}

	handled, err := tango.DispatchFlags(config, sqlDB, db.SQLite, migrations.Migrations)
	if handled || err != nil {
		return err
	}

	fmt.Println("listening on", config.Addr)
	return tango.Serve(config, sqlDB, db.SQLite)
}

func appConfig(store *db.Store) tango.Config {
	return tango.Config{
		InstalledApps: []tango.App{
			&NotesApp{store: store},
			admin.New(store),
		},
		Addr: ":8000",
	}
}

// buildHandler compiles config into a servable http.Handler exactly the
// way tango.Serve does (including wiring store into the registry), split
// out here so tests can exercise real routing/middleware (including
// createLimiter) against an httptest server without going through main's
// flag/env/serve plumbing.
func buildHandler(config tango.Config, store *db.Store) (http.Handler, error) {
	registry, err := tango.BuildRegistry(config)
	if err != nil {
		return nil, err
	}
	if err := registry.RunRegistration(); err != nil {
		return nil, err
	}
	registry.SetStore(store)
	return registry.Routes().Handler()
}

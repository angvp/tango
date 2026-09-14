package tango_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	_ "modernc.org/sqlite"
)

type configCheckApp struct {
	name   string
	checks []tango.AppCheck
}

func (a configCheckApp) Name() string { return a.name }

func (a configCheckApp) Register(*tango.Registry) error { return nil }

func (a configCheckApp) Checks() []tango.AppCheck { return a.checks }

func TestLoadConfigFromEnvDefaultsAddr(t *testing.T) {
	t.Setenv("TANGO_ADDR", "")

	config := tango.LoadConfigFromEnv()

	if config.Addr != ":8000" {
		t.Fatalf("Addr = %q, want %q", config.Addr, ":8000")
	}
}

func TestLoadConfigFromEnvReadsAddr(t *testing.T) {
	t.Setenv("TANGO_ADDR", ":9000")

	config := tango.LoadConfigFromEnv()

	if config.Addr != ":9000" {
		t.Fatalf("Addr = %q, want %q", config.Addr, ":9000")
	}
}

func TestBuildRegistryRegistersInstalledApps(t *testing.T) {
	var registered bool
	app := tango.NewApp("users", func(registry *tango.Registry) error {
		registered = true
		return nil
	})

	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{app}})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	if !registered {
		t.Fatal("installed app registration did not run")
	}
}

func TestBuildRegistryDuplicateAppFails(t *testing.T) {
	app := tango.NewApp("users", func(registry *tango.Registry) error {
		return nil
	})

	_, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{app, app}})

	if !errors.Is(err, tango.ErrDuplicateApp) {
		t.Fatalf("error = %v, want ErrDuplicateApp", err)
	}
}

func TestConfigMiddlewareWrapsBuiltRegistryRoutes(t *testing.T) {
	type orderKey struct{}
	record := func(marker string) tango.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current, _ := r.Context().Value(orderKey{}).([]string)
				current = append(append([]string(nil), current...), marker)
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), orderKey{}, current)))
			})
		}
	}

	app := tango.NewApp("users", func(registry *tango.Registry) error {
		return registry.Routes().Include("/users/", tango.URLs{
			tango.Path("GET", "/", func(ctx *tango.Context) error {
				order := append(ctx.Request().Context().Value(orderKey{}).([]string), "view")
				return ctx.JSON(http.StatusOK, map[string][]string{"order": order})
			}),
		})
	})

	registry, err := tango.BuildRegistry(tango.Config{
		InstalledApps: []tango.App{app},
		Middleware:    []tango.Middleware{record("global")},
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))

	var body map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	want := []string{"global", "view"}
	if !reflect.DeepEqual(body["order"], want) {
		t.Fatalf("order = %v, want %v", body["order"], want)
	}
}

func TestMiddlewareComposesAcrossAppAndAdminTiers(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := db.NewStore(sqlDB, db.SQLite)

	headerOrder := func(marker string) tango.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Middleware-Order", marker)
				next.ServeHTTP(w, r)
			})
		}
	}
	publicApp := tango.NewApp("public", func(registry *tango.Registry) error {
		return registry.Routes().Include("/public/", tango.URLs{
			tango.Path(http.MethodGet, "/", func(ctx *tango.Context) error {
				ctx.ResponseWriter().Header().Add("X-Middleware-Order", "view")
				return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
			}, tango.Use(headerOrder("route"))),
		}, tango.WithMiddleware(headerOrder("group")))
	})

	registry, err := tango.BuildRegistry(tango.Config{
		InstalledApps: []tango.App{
			publicApp,
			admin.New(store, admin.WithMiddleware(headerOrder("admin"))),
		},
		Middleware: []tango.Middleware{headerOrder("global")},
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, httptest.NewRequest(http.MethodGet, "/public/", nil))
	if got, want := publicResponse.Header().Values("X-Middleware-Order"), []string{"global", "group", "route", "view"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("public middleware order = %v, want %v", got, want)
	}

	adminResponse := httptest.NewRecorder()
	handler.ServeHTTP(adminResponse, httptest.NewRequest(http.MethodGet, "/admin/login/", nil))
	if got, want := adminResponse.Header().Values("X-Middleware-Order"), []string{"global", "admin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("admin middleware order = %v, want %v", got, want)
	}
}

func TestCheckRunsRegistrationAndCompilesRoutes(t *testing.T) {
	app := tango.NewApp("users", func(registry *tango.Registry) error {
		return registry.Routes().Include("/users/", tango.URLs{
			tango.Path("GET", "/", func(ctx *tango.Context) error {
				return ctx.JSON(200, map[string]string{"ok": "true"})
			}, tango.Name("list")),
		})
	})

	if err := tango.Check(tango.Config{InstalledApps: []tango.App{app}}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckPassesWhenAppChecksPass(t *testing.T) {
	app := configCheckApp{
		name: "widgets",
		checks: []tango.AppCheck{
			{Description: "widgets configured"},
		},
	}

	if err := tango.Check(tango.Config{InstalledApps: []tango.App{app}}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckReturnsAggregatedAppCheckFailures(t *testing.T) {
	app := configCheckApp{
		name: "widgets",
		checks: []tango.AppCheck{
			{Description: "database configured", Err: errors.New("missing DSN")},
			{Description: "templates available"},
			{Description: "admin credentials", Err: errors.New("missing password")},
		},
	}

	err := tango.Check(tango.Config{InstalledApps: []tango.App{app}})
	if err == nil {
		t.Fatal("Check returned nil, want app check failure")
	}
	message := err.Error()
	for _, want := range []string{"database configured", "missing DSN", "admin credentials", "missing password"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want it to contain %q", message, want)
		}
	}
	if strings.Contains(message, "templates available") {
		t.Fatalf("error = %q, passing check should not be listed", message)
	}
}

func TestCheckReturnsRegistrationError(t *testing.T) {
	expected := errors.New("boom")
	app := tango.NewApp("bad", func(registry *tango.Registry) error {
		return expected
	})

	err := tango.Check(tango.Config{InstalledApps: []tango.App{app}})

	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
}

type checkFKAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

type checkFKPost struct {
	ID       int64 `tango:"pk"`
	AuthorID int64 `tango:"fk=checkFKAuthor"`
}

func TestCheckPassesWithValidForeignKeyRegardlessOfInstalledAppsOrder(t *testing.T) {
	posts := tango.NewApp("posts", func(registry *tango.Registry) error {
		return registry.Models().Register(checkFKPost{})
	})
	authors := tango.NewApp("authors", func(registry *tango.Registry) error {
		return registry.Models().Register(checkFKAuthor{})
	})

	// posts (the referencing app) installed before authors (the referenced
	// app): foreign-key targets are validated once after every app has
	// finished registering, not at Register time, so InstalledApps order
	// must not affect whether Check passes.
	if err := tango.Check(tango.Config{InstalledApps: []tango.App{posts, authors}}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckFailsOnDanglingForeignKeyTarget(t *testing.T) {
	posts := tango.NewApp("posts", func(registry *tango.Registry) error {
		return registry.Models().Register(checkFKPost{})
	})
	// checkFKAuthor is never registered by any app.

	err := tango.Check(tango.Config{InstalledApps: []tango.App{posts}})
	if err == nil {
		t.Fatal("Check returned nil error, want an error naming the missing foreign key target")
	}
	if !strings.Contains(err.Error(), "checkFKAuthor") {
		t.Fatalf("error = %q, want it to name the missing model %q", err.Error(), "checkFKAuthor")
	}
}

type dumpModelsWidget struct {
	ID   int64 `tango:"pk"`
	Name string
}

func TestDumpModelsReturnsRegisteredModels(t *testing.T) {
	app := tango.NewApp("widgets", func(registry *tango.Registry) error {
		return registry.Models().Register(dumpModelsWidget{})
	})

	models, err := tango.DumpModels(tango.Config{InstalledApps: []tango.App{app}})
	if err != nil {
		t.Fatalf("DumpModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].App != "widgets" {
		t.Fatalf("App = %q, want %q", models[0].App, "widgets")
	}
	if models[0].Name != "dump_models_widget" {
		t.Fatalf("Name = %q, want %q", models[0].Name, "dump_models_widget")
	}
}

func TestDumpModelsReturnsRegistrationError(t *testing.T) {
	expected := errors.New("boom")
	app := tango.NewApp("bad", func(registry *tango.Registry) error {
		return expected
	})

	_, err := tango.DumpModels(tango.Config{InstalledApps: []tango.App{app}})
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
}

func TestCheckReturnsRouteCompileError(t *testing.T) {
	first := tango.NewApp("one", func(registry *tango.Registry) error {
		return registry.Routes().Include("/users/", tango.URLs{
			tango.Path("GET", "/", func(ctx *tango.Context) error { return nil }, tango.Name("list")),
		})
	})
	second := tango.NewApp("two", func(registry *tango.Registry) error {
		return registry.Routes().Include("/users/", tango.URLs{
			tango.Path("GET", "/active/", func(ctx *tango.Context) error { return nil }, tango.Name("list")),
		})
	})

	err := tango.Check(tango.Config{InstalledApps: []tango.App{first, second}})

	if !errors.Is(err, tango.ErrDuplicateRouteName) {
		t.Fatalf("error = %v, want ErrDuplicateRouteName", err)
	}
}

func TestStatusReportsHealthyProjectWithNoPendingMigrations(t *testing.T) {
	app := tango.NewApp("widgets", func(registry *tango.Registry) error {
		return nil
	})
	config := tango.Config{InstalledApps: []tango.App{app}}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	migrations := []migration.Migration{
		{App: "widgets", Name: "0001_auto", Reversible: true, Up: []migration.Step{
			migration.CreateTable{Table: "widget", Columns: []migration.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
			}},
		}},
	}
	if err := migration.ApplyPending(context.Background(), sqlDB, db.SQLite, migrations); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	status := tango.Status(context.Background(), config, sqlDB, db.SQLite, migrations)

	if !status.RegistrationOK || status.RegistrationError != "" {
		t.Fatalf("RegistrationOK/Error = %v/%q, want true/\"\"", status.RegistrationOK, status.RegistrationError)
	}
	if !status.DatabaseReachable || status.DatabaseError != "" {
		t.Fatalf("DatabaseReachable/Error = %v/%q, want true/\"\"", status.DatabaseReachable, status.DatabaseError)
	}
	if status.MigrationsTotal != 1 || status.MigrationsApplied != 1 || status.MigrationsPending != 0 {
		t.Fatalf("migration counts = %+v, want total=1 applied=1 pending=0", status)
	}
}

func TestStatusReportsRegistrationError(t *testing.T) {
	expected := errors.New("boom")
	app := tango.NewApp("bad", func(registry *tango.Registry) error {
		return expected
	})
	config := tango.Config{InstalledApps: []tango.App{app}}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	status := tango.Status(context.Background(), config, sqlDB, db.SQLite, nil)

	if status.RegistrationOK {
		t.Fatal("RegistrationOK = true, want false")
	}
	if status.RegistrationError == "" {
		t.Fatal("RegistrationError is empty, want the underlying error message")
	}
}

func TestStatusReportsDatabaseUnreachableWithoutPanicking(t *testing.T) {
	app := tango.NewApp("widgets", func(registry *tango.Registry) error {
		return nil
	})
	config := tango.Config{InstalledApps: []tango.App{app}}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB.Close()

	status := tango.Status(context.Background(), config, sqlDB, db.SQLite, nil)

	if status.DatabaseReachable {
		t.Fatal("DatabaseReachable = true, want false for a closed database")
	}
	if status.DatabaseError == "" {
		t.Fatal("DatabaseError is empty, want the underlying error message")
	}
}

func TestStatusIsReadOnlyWhenTrackingTableIsMissing(t *testing.T) {
	app := tango.NewApp("widgets", func(registry *tango.Registry) error {
		return nil
	})
	config := tango.Config{InstalledApps: []tango.App{app}}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	migrations := []migration.Migration{
		{App: "widgets", Name: "0001_auto", Reversible: true},
		{App: "widgets", Name: "0002_auto", Reversible: true},
	}

	status := tango.Status(context.Background(), config, sqlDB, db.SQLite, migrations)

	if !status.DatabaseReachable || status.DatabaseError != "" {
		t.Fatalf("DatabaseReachable/Error = %v/%q, want true/\"\"", status.DatabaseReachable, status.DatabaseError)
	}
	if status.MigrationsTotal != 2 || status.MigrationsApplied != 0 || status.MigrationsPending != 2 {
		t.Fatalf("migration counts = %+v, want total=2 applied=0 pending=2", status)
	}

	var tableName string
	err = sqlDB.QueryRowContext(
		context.Background(),
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'tango_migrations'",
	).Scan(&tableName)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("tango_migrations table query error = %v, want sql.ErrNoRows", err)
	}
}

func TestStatusReportsPendingMigrations(t *testing.T) {
	app := tango.NewApp("widgets", func(registry *tango.Registry) error {
		return nil
	})
	config := tango.Config{InstalledApps: []tango.App{app}}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	applied := []migration.Migration{
		{App: "widgets", Name: "0001_auto", Reversible: true, Up: []migration.Step{
			migration.CreateTable{Table: "widget", Columns: []migration.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
			}},
		}},
	}
	if err := migration.ApplyPending(context.Background(), sqlDB, db.SQLite, applied); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	allMigrations := append(applied, migration.Migration{
		App: "widgets", Name: "0002_auto", Reversible: true, Up: []migration.Step{
			migration.AddColumn{Table: "widget", Column: migration.Column{Name: "name", Type: "text"}},
		},
	})

	status := tango.Status(context.Background(), config, sqlDB, db.SQLite, allMigrations)

	if status.MigrationsTotal != 2 || status.MigrationsApplied != 1 || status.MigrationsPending != 1 {
		t.Fatalf("migration counts = %+v, want total=2 applied=1 pending=1", status)
	}
}

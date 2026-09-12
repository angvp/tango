package tango_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	_ "modernc.org/sqlite"
)

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

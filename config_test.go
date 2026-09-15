package tango_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestStatusReportsNonMissingTableDatabaseError covers the branch where
// AppliedMigrations fails for a reason other than the tracking table simply
// not existing yet (e.g. it exists but its schema is wrong) — Status must
// still surface this as a DatabaseError rather than mistake it for the
// read-only "no tracking table yet" case.
func TestStatusReportsNonMissingTableDatabaseError(t *testing.T) {
	app := tango.NewApp("widgets", func(registry *tango.Registry) error {
		return nil
	})
	config := tango.Config{InstalledApps: []tango.App{app}}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	// A tracking table that exists but lacks the columns AppliedMigrations
	// queries: its error message will not contain "no such table".
	if _, err := sqlDB.Exec("CREATE TABLE tango_migrations (id INTEGER)"); err != nil {
		t.Fatalf("create malformed tracking table: %v", err)
	}

	status := tango.Status(context.Background(), config, sqlDB, db.SQLite, nil)

	if status.DatabaseError == "" {
		t.Fatal("DatabaseError is empty, want the underlying query error")
	}
	if status.MigrationsPending != 0 {
		t.Fatalf("MigrationsPending = %d, want 0 (Status must not guess pending count on a non-missing-table error)", status.MigrationsPending)
	}
}

// TestLoadEnvFileErrorHandling table-drives LoadEnvFile's error-returning
// branches: a missing file (a no-op), a directory given as the path, a line
// missing "=", and a key os.Setenv itself rejects (a NUL byte).
func TestLoadEnvFileErrorHandling(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(t *testing.T, dir string) string // returns the path to load
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "missing file is a noop",
			setup: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "does-not-exist.env")
			},
		},
		{
			name: "path is a directory",
			setup: func(t *testing.T, dir string) string {
				return dir
			},
			wantErr: true,
		},
		{
			name: "line without equals",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, ".env")
				if err := os.WriteFile(path, []byte("NOT_KEY_VALUE\n"), 0o644); err != nil {
					t.Fatalf("write env file: %v", err)
				}
				return path
			},
			wantErr:         true,
			wantErrContains: "expected KEY=VALUE",
		},
		{
			// Covers the (rare) case where a parsed key isn't a valid
			// environment variable name: a NUL byte makes os.Setenv itself
			// reject it, and that failure must propagate rather than being
			// swallowed.
			name: "setenv rejects a NUL byte in the key",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, ".env")
				if err := os.WriteFile(path, []byte("BAD\x00KEY=value\n"), 0o644); err != nil {
					t.Fatalf("write env file: %v", err)
				}
				return path
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tt.setup(t, dir)

			err := tango.LoadEnvFile(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoadEnvFile error = nil, want non-nil")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("LoadEnvFile error = %v, want it to contain %q", err, tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadEnvFile: %v, want nil", err)
			}
		})
	}
}

// TestLoadEnvFileSetsEnvironmentVariables table-drives LoadEnvFile's
// successful parsing: unset variables get set (comments/blank lines
// skipped), and already-set variables are left untouched.
func TestLoadEnvFileSetsEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name    string
		content string
		preset  map[string]string
		wantEnv map[string]string
	}{
		{
			name:    "sets unset variables and skips comments and blank lines",
			content: "# a comment\n\nTANGO_TEST_ENV_LOAD_A=\"quoted value\"\nTANGO_TEST_ENV_LOAD_B=plain\n",
			wantEnv: map[string]string{
				"TANGO_TEST_ENV_LOAD_A": "quoted value",
				"TANGO_TEST_ENV_LOAD_B": "plain",
			},
		},
		{
			name:    "does not override already set variables",
			content: "TANGO_TEST_ENV_LOAD_PRESET=from_file\n",
			preset:  map[string]string{"TANGO_TEST_ENV_LOAD_PRESET": "from_environment"},
			wantEnv: map[string]string{"TANGO_TEST_ENV_LOAD_PRESET": "from_environment"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".env")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write env file: %v", err)
			}

			for key := range tt.wantEnv {
				if _, preset := tt.preset[key]; !preset {
					os.Unsetenv(key)
				}
			}
			for key, value := range tt.preset {
				t.Setenv(key, value)
			}
			t.Cleanup(func() {
				for key := range tt.wantEnv {
					os.Unsetenv(key)
				}
			})

			if err := tango.LoadEnvFile(path); err != nil {
				t.Fatalf("LoadEnvFile: %v", err)
			}
			for key, want := range tt.wantEnv {
				if got := os.Getenv(key); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	original := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = original })
	fn()
}

// TestDispatchFlagsReportsFailures table-drives DispatchFlags's error paths
// across its different flags: an unknown flag, and each handled flag
// propagating an underlying failure (registration, check, or migration).
func TestDispatchFlagsReportsFailures(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		config tango.Config
		db     func(t *testing.T) *sql.DB
		check  func(t *testing.T, err error)
	}{
		{
			name: "rejects unknown flag",
			args: []string{"tango", "--bogus"},
			check: func(t *testing.T, err error) {
				if err == nil {
					t.Fatal("err = nil, want non-nil for an unknown flag")
				}
			},
		},
		{
			name: "dump models reports registration error",
			args: []string{"tango", "-tango-dump-models"},
			config: tango.Config{InstalledApps: []tango.App{
				tango.NewApp("dup", func(registry *tango.Registry) error { return nil }),
				tango.NewApp("dup", func(registry *tango.Registry) error { return nil }),
			}},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, tango.ErrDuplicateApp) {
					t.Fatalf("err = %v, want ErrDuplicateApp", err)
				}
			},
		},
		{
			name: "check reports failure",
			args: []string{"tango", "-check"},
			config: tango.Config{InstalledApps: []tango.App{
				tango.NewApp("bad", func(registry *tango.Registry) error { return errors.New("boom") }),
			}},
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "check failed:") {
					t.Fatalf("err = %v, want \"check failed: ...\"", err)
				}
			},
		},
		{
			name: "migrate down reports no applied migrations",
			args: []string{"tango", "-migrate", "-down"},
			db: func(t *testing.T) *sql.DB {
				sqlDB, err := sql.Open("sqlite", ":memory:")
				if err != nil {
					t.Fatalf("open sqlite: %v", err)
				}
				t.Cleanup(func() { _ = sqlDB.Close() })
				return sqlDB
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, migration.ErrNoAppliedMigrations) {
					t.Fatalf("err = %v, want ErrNoAppliedMigrations", err)
				}
			},
		},
		{
			name: "migrate reports apply failure",
			args: []string{"tango", "-migrate"},
			db: func(t *testing.T) *sql.DB {
				sqlDB, err := sql.Open("sqlite", ":memory:")
				if err != nil {
					t.Fatalf("open sqlite: %v", err)
				}
				sqlDB.Close()
				return sqlDB
			},
			check: func(t *testing.T, err error) {
				if err == nil {
					t.Fatal("err = nil, want non-nil for a closed database")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sqlDB *sql.DB
			if tt.db != nil {
				sqlDB = tt.db(t)
			}
			withArgs(t, tt.args, func() {
				handled, err := tango.DispatchFlags(tt.config, sqlDB, db.SQLite, nil)
				if !handled {
					t.Fatal("handled = false, want true")
				}
				tt.check(t, err)
			})
		})
	}
}

// TestBuildRegistryErrorPropagatesToCheckAndDumpModels covers Check and
// DumpModels both surfacing the same underlying BuildRegistry error
// (ErrDuplicateApp) rather than swallowing or rewrapping it.
func TestBuildRegistryErrorPropagatesToCheckAndDumpModels(t *testing.T) {
	tests := []struct {
		name string
		call func(config tango.Config) error
	}{
		{
			name: "Check",
			call: func(config tango.Config) error { return tango.Check(config) },
		},
		{
			name: "DumpModels",
			call: func(config tango.Config) error {
				_, err := tango.DumpModels(config)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := tango.NewApp("dup", func(registry *tango.Registry) error { return nil })
			err := tt.call(tango.Config{InstalledApps: []tango.App{app, app}})
			if !errors.Is(err, tango.ErrDuplicateApp) {
				t.Fatalf("error = %v, want ErrDuplicateApp", err)
			}
		})
	}
}

func TestCheckReturnsAggregatedAppCheckFailureWithDefaultDescription(t *testing.T) {
	app := configCheckApp{
		name: "widgets",
		checks: []tango.AppCheck{
			{Err: errors.New("boom")},
		},
	}

	err := tango.Check(tango.Config{InstalledApps: []tango.App{app}})
	if err == nil || !strings.Contains(err.Error(), "unnamed app check") {
		t.Fatalf("error = %v, want it to fall back to \"unnamed app check\"", err)
	}
}

// TestServeWrapsErrorsFromEachStage covers Serve wrapping the underlying
// error from each stage of building and running an app in turn: building the
// registry, running registration, and compiling routes.
func TestServeWrapsErrorsFromEachStage(t *testing.T) {
	tests := []struct {
		name          string
		config        tango.Config
		wantErrPrefix string
	}{
		{
			name: "build registry error",
			config: tango.Config{InstalledApps: []tango.App{
				tango.NewApp("dup", func(registry *tango.Registry) error { return nil }),
				tango.NewApp("dup", func(registry *tango.Registry) error { return nil }),
			}},
			wantErrPrefix: "build registry:",
		},
		{
			name: "run registration error",
			config: tango.Config{InstalledApps: []tango.App{
				tango.NewApp("bad", func(registry *tango.Registry) error { return errors.New("boom") }),
			}},
			wantErrPrefix: "run registration:",
		},
		{
			name: "route compile error",
			config: tango.Config{InstalledApps: []tango.App{
				tango.NewApp("one", func(registry *tango.Registry) error {
					return registry.Routes().Include("/users/", tango.URLs{
						tango.Path("GET", "/", func(ctx *tango.Context) error { return nil }, tango.Name("list")),
					})
				}),
				tango.NewApp("two", func(registry *tango.Registry) error {
					return registry.Routes().Include("/users/", tango.URLs{
						tango.Path("GET", "/active/", func(ctx *tango.Context) error { return nil }, tango.Name("list")),
					})
				}),
			}},
			wantErrPrefix: "compile routes:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tango.Serve(tt.config, nil, db.SQLite)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrPrefix) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErrPrefix)
			}
		})
	}
}

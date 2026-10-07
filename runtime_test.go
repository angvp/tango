package tango

import (
	"context"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	_ "modernc.org/sqlite"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = writer
	callErr := fn()
	writer.Close()
	os.Stdout = original
	output, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(output), callErr
}

func withArgs(t *testing.T, args ...string) {
	t.Helper()
	original := os.Args
	os.Args = append([]string{"app"}, args...)
	t.Cleanup(func() { os.Args = original })
}

func TestDispatchFlagsReturnsUnhandledWithoutTangoFlag(t *testing.T) {
	withArgs(t)
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	handled, err := DispatchFlags(Config{}, sqlDB, db.SQLite, nil)
	if handled || err != nil {
		t.Fatalf("handled, err = %v, %v; want false, nil", handled, err)
	}
}

func TestDispatchFlagsCheck(t *testing.T) {
	withArgs(t, "-check")
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	output, err := captureStdout(t, func() error {
		handled, err := DispatchFlags(Config{}, sqlDB, db.SQLite, nil)
		if !handled {
			t.Fatal("handled = false, want true")
		}
		return err
	})
	if err != nil {
		t.Fatalf("DispatchFlags returned error: %v", err)
	}
	if strings.TrimSpace(output) != "check passed" {
		t.Fatalf("stdout = %q, want check passed", output)
	}
}

func TestDispatchFlagsStatusIsJSON(t *testing.T) {
	withArgs(t, "-tango-status")
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	output, err := captureStdout(t, func() error {
		handled, err := DispatchFlags(Config{}, sqlDB, db.SQLite, nil)
		if !handled {
			t.Fatal("handled = false, want true")
		}
		return err
	})
	if err != nil {
		t.Fatalf("DispatchFlags returned error: %v", err)
	}
	if !strings.Contains(output, `"registrationOk":true`) {
		t.Fatalf("stdout = %q, want status JSON", output)
	}
}

func TestDispatchFlagsMigrateAndDown(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()
	migrations := []migration.Migration{{App: "widgets", Name: "0001_auto", Reversible: true, Up: []migration.Step{
		migration.CreateTable{Table: "widget", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}, Down: []migration.Step{migration.DropTable{Table: "widget"}}}}

	withArgs(t, "-migrate")
	output, err := captureStdout(t, func() error {
		handled, err := DispatchFlags(Config{}, sqlDB, db.SQLite, migrations)
		if !handled {
			t.Fatal("handled = false, want true")
		}
		return err
	})
	if err != nil {
		t.Fatalf("DispatchFlags migrate returned error: %v", err)
	}
	if !strings.Contains(output, "migrations applied") {
		t.Fatalf("stdout = %q, want migrations applied", output)
	}

	os.Args = []string{"app", "-migrate", "-down"}
	output, err = captureStdout(t, func() error {
		handled, err := DispatchFlags(Config{}, sqlDB, db.SQLite, migrations)
		if !handled {
			t.Fatal("handled = false, want true")
		}
		return err
	})
	if err != nil {
		t.Fatalf("DispatchFlags migrate down returned error: %v", err)
	}
	if !strings.Contains(output, "rolled back last migration") {
		t.Fatalf("stdout = %q, want rolled back last migration", output)
	}
}

func TestServeBuildsRegistryAndListens(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0"}, sqlDB, db.SQLite)
	}()

	// ServeContext's bind happens synchronously inside it before it ever
	// blocks, but nothing signals this test the instant that's done — give
	// it a moment, then cancel to trigger a graceful, no-op shutdown.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeContext error = %v, want nil for a clean caller-triggered shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeContext did not return after ctx cancellation")
	}
}

func TestLoadDBConfigFromEnvDefaultsToSQLiteAppDB(t *testing.T) {
	t.Setenv("TANGO_DB_DSN", "")
	t.Setenv("TANGO_DB_DIALECT", "")
	got, err := LoadDBConfigFromEnv()
	want := db.DSN{Dialect: db.SQLite, Driver: "sqlite", Source: "app.db?_foreign_keys=on"}
	if err != nil || got != want {
		t.Fatalf("LoadDBConfigFromEnv = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestLoadDBConfigFromEnvSelectsDialectFromScheme(t *testing.T) {
	const dsn = "postgres://tango:tango@localhost:5432/app?sslmode=disable"
	t.Setenv("TANGO_DB_DSN", dsn)
	got, err := LoadDBConfigFromEnv()
	want := db.DSN{Dialect: db.Postgres, Driver: "pgx", Source: dsn}
	if err != nil || got != want {
		t.Fatalf("LoadDBConfigFromEnv = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestLoadDBConfigFromEnvRejectsBarePath(t *testing.T) {
	t.Setenv("TANGO_DB_DSN", "app.db")
	if _, err := LoadDBConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "TANGO_DB_DSN") {
		t.Fatalf("LoadDBConfigFromEnv error = %v, want an error naming TANGO_DB_DSN", err)
	}
}

func TestLoadDBConfigFromEnvFailsWhenRetiredDialectVariableIsSet(t *testing.T) {
	t.Setenv("TANGO_DB_DSN", "postgres://tango:tango@localhost:5432/app")
	t.Setenv("TANGO_DB_DIALECT", "postgres")
	_, err := LoadDBConfigFromEnv()
	want := "TANGO_DB_DIALECT was removed; put the scheme in TANGO_DB_DSN (e.g. postgres://…)"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadDBConfigFromEnv error = %v, want it to contain %q", err, want)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.env"
	if err := os.WriteFile(path, []byte("TANGO_ADMIN_PASSWORD=\"from-file\"\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("TANGO_ADMIN_PASSWORD", "")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}
	if got := os.Getenv("TANGO_ADMIN_PASSWORD"); got != "from-file" {
		t.Fatalf("env = %q, want from-file", got)
	}
}

func TestDispatchFlagsDumpModels(t *testing.T) {
	withArgs(t, "-tango-dump-models")
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	output, err := captureStdout(t, func() error {
		handled, err := DispatchFlags(Config{}, sqlDB, db.SQLite, nil)
		if !handled {
			t.Fatal("handled = false, want true")
		}
		return err
	})
	if err != nil {
		t.Fatalf("DispatchFlags returned error: %v", err)
	}
	if strings.TrimSpace(output) != "[]" {
		t.Fatalf("stdout = %q, want [] JSON", output)
	}
}

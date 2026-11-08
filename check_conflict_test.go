package tango_test

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

func validMigrations() []migration.Migration {
	return []migration.Migration{
		{App: "blog", Name: "0001_initial", Reversible: true,
			Up:   []migration.Step{migration.CreateTable{Table: "dc_post", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []migration.Step{migration.DropTable{Table: "dc_post"}}},
	}
}

// The database is closed before -check runs. -check must not read it, so a
// database error can never be what the test sees.
func TestDispatchFlagsCheckFailsOnMigrationConflict(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	sqlDB.Close()

	withArgs(t, []string{"tango", "-check"}, func() {
		handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, dialect, conflictingMigrations())
		if !handled {
			t.Fatal("handled = false, want true")
		}
		if !errors.Is(err, migration.ErrConflictingMigrations) {
			t.Fatalf("err = %v, want ErrConflictingMigrations", err)
		}
		if !strings.HasPrefix(err.Error(), "check failed: ") {
			t.Fatalf("err = %q, want the check failed: prefix", err.Error())
		}
	})
}

func TestDispatchFlagsCheckReportsConfigAndMigrationFailuresTogether(t *testing.T) {
	sentinel := errors.New("registration boom")
	config := tango.Config{InstalledApps: []tango.App{
		tango.NewApp("bad", func(registry *tango.Registry) error { return sentinel }),
	}}
	sqlDB, dialect := testdb.Open(t)
	sqlDB.Close()

	withArgs(t, []string{"tango", "-check"}, func() {
		handled, err := tango.DispatchFlags(config, sqlDB, dialect, conflictingMigrations())
		if !handled {
			t.Fatal("handled = false, want true")
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want the registration failure", err)
		}
		if !errors.Is(err, migration.ErrConflictingMigrations) {
			t.Fatalf("err = %v, want ErrConflictingMigrations alongside the registration failure", err)
		}
	})
}

func TestDispatchFlagsCheckPassesValidRegistryWithoutTouchingDatabase(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	sqlDB.Close()

	withArgs(t, []string{"tango", "-check"}, func() {
		output, err := captureCheckStdout(t, func() error {
			handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, dialect, validMigrations())
			if !handled {
				t.Fatal("handled = false, want true")
			}
			return err
		})
		if err != nil {
			t.Fatalf("DispatchFlags returned error: %v", err)
		}
		if got := strings.TrimSpace(output); got != "check passed" {
			t.Fatalf("stdout = %q, want check passed", got)
		}
	})
}

// captureCheckStdout returns what fn writes to os.Stdout. It is local because
// captureStdout lives in the package tango test files, not this package.
func captureCheckStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer reader.Close()
	original := os.Stdout
	var runErr error
	func() {
		// The defer runs even when fn calls t.Fatal, so os.Stdout is restored
		// and the writer closed before any later test runs.
		defer func() {
			os.Stdout = original
			if err := writer.Close(); err != nil {
				t.Errorf("closing stdout pipe: %v", err)
			}
		}()
		os.Stdout = writer
		runErr = fn()
	}()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading stdout pipe: %v", err)
	}
	return string(output), runErr
}

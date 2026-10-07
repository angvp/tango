package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/testdb"
)

// TestRollbackLastSurfacesEnsureTrackingTableError forces
// EnsureTrackingTable's CREATE TABLE to fail (a closed connection stands in
// for any ordinary driver-level failure) and confirms RollbackLast
// propagates it.
func TestRollbackLastSurfacesEnsureTrackingTableError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err := RollbackLast(context.Background(), sqlDB, dialect, nil)
	if err == nil {
		t.Fatal("RollbackLast on a closed connection returned nil error, want an error")
	}
}

func TestRollbackLastSurfacesLastAppliedKeyQueryError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	// A tango_migrations table that exists but is missing the "name"
	// column: EnsureTrackingTable's CREATE TABLE IF NOT EXISTS leaves it
	// alone, and lastAppliedKey's SELECT app, name then fails with an
	// ordinary "no such column" error rather than sql.ErrNoRows.
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE tango_migrations (app TEXT NOT NULL)`); err != nil {
		t.Fatalf("create broken tracking table: %v", err)
	}

	err := RollbackLast(ctx, sqlDB, dialect, nil)
	if err == nil {
		t.Fatal("RollbackLast with a malformed tango_migrations table returned nil error, want an error")
	}
	if errors.Is(err, ErrNoAppliedMigrations) {
		t.Fatalf("error = %v, want a plain SQL error, not ErrNoAppliedMigrations", err)
	}
}

func TestRollbackLastMigrationNotFoundAmongProvidedReturnsError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	applied := []Migration{
		{Name: "0001_create_account", App: "users", Reversible: true, Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}, Down: []Step{DropTable{Table: "account"}}},
	}
	if err := ApplyPending(ctx, sqlDB, dialect, applied); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	// A different, unrelated migration set: the applied one isn't in it.
	err := RollbackLast(ctx, sqlDB, dialect, []Migration{
		{Name: "9999_something_else", App: "other", Reversible: true},
	})
	if err == nil {
		t.Fatal("RollbackLast returned nil error for a migration missing from the provided set, want an error")
	}
}

func TestRollbackLastDownStepFailureSurfacesError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_create_account", App: "users", Reversible: true, Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}, Down: []Step{
			// DropTable on a table that was never created: ApplyStep fails.
			DropTable{Table: "does_not_exist"},
		}},
	}
	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	err := RollbackLast(ctx, sqlDB, dialect, migrations)
	if err == nil {
		t.Fatal("RollbackLast returned nil error for a failing Down step, want an error")
	}
}

// TestRollbackLastTrackingRowDeletionFailureSurfacesError forces the final
// DELETE FROM tango_migrations to fail after the Down steps already ran, by
// blocking deletes on tango_migrations with an ordinary SQL trigger — a
// realistic scenario (e.g. an audit trigger) rather than a contrived fault.
func TestRollbackLastTrackingRowDeletionFailureSurfacesError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_create_account", App: "users", Reversible: true, Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}, Down: []Step{
			DropTable{Table: "account"},
		}},
	}
	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	blockTrackingTable(t, sqlDB, dialect, "DELETE")

	err := RollbackLast(ctx, sqlDB, dialect, migrations)
	if err == nil {
		t.Fatal("RollbackLast returned nil error when the tracking-row DELETE is blocked, want an error")
	}
}

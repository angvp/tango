package tango_test

import (
	"context"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

// exactDuplicateError is the exact MigrationError text for a registry that
// registers the same (App, Name) twice.
const exactDuplicateError = "tango migration: conflicting migration names:\n- app \"blog\": name \"0005_add_email\" is registered 2 times; remove the duplicate registration or rename one migration"

// One tracking row for an exact duplicate is one applied migration, and the
// duplicated registration is one identity in the total.
func TestStatusCountsExactDuplicateAsOneMigration(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	first := conflictingMigrations()[0]
	if err := migration.ApplyPending(ctx, sqlDB, dialect, []migration.Migration{first}); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	registry := []migration.Migration{first, first}
	status := tango.Status(ctx, widgetsConfig(), sqlDB, dialect, registry)

	if status.MigrationError != exactDuplicateError {
		t.Fatalf("MigrationError = %q, want %q", status.MigrationError, exactDuplicateError)
	}
	if !status.DatabaseReachable {
		t.Fatalf("DatabaseReachable = false, want true: DatabaseError = %q", status.DatabaseError)
	}
	if status.MigrationsTotal != 1 {
		t.Fatalf("MigrationsTotal = %d, want 1", status.MigrationsTotal)
	}
	if status.MigrationsApplied != 1 {
		t.Fatalf("MigrationsApplied = %d, want 1", status.MigrationsApplied)
	}
	if status.MigrationsPending != 0 {
		t.Fatalf("MigrationsPending = %d, want 0", status.MigrationsPending)
	}
}

// Numeric-prefix conflicts with different names are distinct identities and
// keep their existing counts.
func TestStatusCountsNumericConflictAsDistinctMigrations(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	first := conflictingMigrations()[0]
	if err := migration.ApplyPending(ctx, sqlDB, dialect, []migration.Migration{first}); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	status := tango.Status(ctx, widgetsConfig(), sqlDB, dialect, conflictingMigrations())

	if status.MigrationError != conflictingMigrationsError {
		t.Fatalf("MigrationError = %q, want %q", status.MigrationError, conflictingMigrationsError)
	}
	if status.MigrationsTotal != 2 || status.MigrationsApplied != 1 || status.MigrationsPending != 1 {
		t.Fatalf("counts total=%d applied=%d pending=%d, want 2/1/1",
			status.MigrationsTotal, status.MigrationsApplied, status.MigrationsPending)
	}
}

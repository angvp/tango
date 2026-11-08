package tango_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/testdb"
)

func widgetsConfig() tango.Config {
	return tango.Config{InstalledApps: []tango.App{
		tango.NewApp("widgets", func(registry *tango.Registry) error { return nil }),
	}}
}

// conflictingMigrationsError is the exact MigrationError text for
// conflictingMigrations.
const conflictingMigrationsError = "tango migration: conflicting migration names:\n- app \"blog\": number 0005 is shared by \"0005_add_email\", \"0005_add_phone\"; renumber one migration"

func TestStatusStillComputesCountsWhenRegistryHasConflict(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	status := tango.Status(context.Background(), widgetsConfig(), sqlDB, dialect, conflictingMigrations())

	if status.MigrationError != conflictingMigrationsError {
		t.Fatalf("MigrationError = %q, want %q", status.MigrationError, conflictingMigrationsError)
	}
	if !status.RegistrationOK {
		t.Fatalf("RegistrationOK = false, want true: the registration itself is valid")
	}
	if !status.DatabaseReachable {
		t.Fatalf("DatabaseReachable = false, want true: DatabaseError = %q", status.DatabaseError)
	}
	if status.MigrationsTotal != 2 {
		t.Fatalf("MigrationsTotal = %d, want 2", status.MigrationsTotal)
	}
	if status.MigrationsPending != 2 {
		t.Fatalf("MigrationsPending = %d, want 2 with nothing applied", status.MigrationsPending)
	}
}

// Status returns early when the database is unreachable, so the validation
// must run before that return or the conflict would go unreported.
func TestStatusReportsMigrationConflictWhenDatabaseIsUnreachable(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	sqlDB.Close()

	status := tango.Status(context.Background(), widgetsConfig(), sqlDB, dialect, conflictingMigrations())

	if status.MigrationError != conflictingMigrationsError {
		t.Fatalf("MigrationError = %q, want %q even with the database closed", status.MigrationError, conflictingMigrationsError)
	}
	if status.DatabaseReachable || status.DatabaseError == "" {
		t.Fatalf("DatabaseReachable = %v, DatabaseError = %q; want the closed database reported", status.DatabaseReachable, status.DatabaseError)
	}
}

func TestStatusLeavesMigrationErrorEmptyForValidRegistry(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	status := tango.Status(context.Background(), widgetsConfig(), sqlDB, dialect, validMigrations())

	if status.MigrationError != "" {
		t.Fatalf("MigrationError = %q, want empty for a valid registry", status.MigrationError)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(encoded), `"migrationError"`) {
		t.Fatalf("JSON %s carries migrationError for a valid registry, want the key omitted", encoded)
	}
}

func TestStatusJSONCarriesMigrationErrorUnderItsKey(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	status := tango.Status(context.Background(), widgetsConfig(), sqlDB, dialect, conflictingMigrations())
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if _, ok := fields["migrationError"]; !ok {
		t.Fatalf("JSON %s has no migrationError key", encoded)
	}
	if _, ok := fields["registrationOk"]; !ok {
		t.Fatalf("JSON %s lost the existing registrationOk key", encoded)
	}
}

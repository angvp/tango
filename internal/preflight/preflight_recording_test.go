package preflight

import (
	"errors"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

// conflictCase pairs each conflict shape Validate reports with the exact text
// -check prints for it, observed from the real Validate output.
type conflictCase struct {
	name     string
	registry []migration.Migration
	checkErr string
}

func conflictCases() []conflictCase {
	return []conflictCase{
		{
			name:     "exact duplicate name",
			registry: duplicateNameMigrations(),
			checkErr: "check failed: tango migration: conflicting migration names:\n" +
				"- app \"blog\": name \"0001_initial\" is registered 2 times; remove the duplicate registration or rename one migration",
		},
		{
			name:     "shared number",
			registry: conflictingMigrations(),
			checkErr: "check failed: tango migration: conflicting migration names:\n" +
				"- app \"blog\": number 0005 is shared by \"0005_add_email\", \"0005_add_phone\"; renumber one migration",
		},
	}
}

func TestCheckRefusesConflictWithoutTouchingDatabase(t *testing.T) {
	for _, tc := range conflictCases() {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, log := newRecordingDB(t)

			withArgs(t, []string{"tango", "-check"}, func() {
				handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, db.SQLite, tc.registry)
				if !handled {
					t.Fatal("handled = false, want true")
				}
				if !errors.Is(err, migration.ErrConflictingMigrations) {
					t.Fatalf("err = %v, want ErrConflictingMigrations", err)
				}
				if err.Error() != tc.checkErr {
					t.Fatalf("err = %q, want %q", err.Error(), tc.checkErr)
				}
			})
			if calls := log.snapshot(); len(calls) != 0 {
				t.Fatalf("database calls = %v, want none on a -check refusal", calls)
			}
		})
	}
}

func TestMigrateRefusesConflictWithoutTouchingDatabase(t *testing.T) {
	for _, tc := range conflictCases() {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, log := newRecordingDB(t)

			withArgs(t, []string{"tango", "-migrate"}, func() {
				handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, db.SQLite, tc.registry)
				if !handled {
					t.Fatal("handled = false, want true")
				}
				if !errors.Is(err, migration.ErrConflictingMigrations) {
					t.Fatalf("err = %v, want ErrConflictingMigrations", err)
				}
			})
			if calls := log.snapshot(); len(calls) != 0 {
				t.Fatalf("database calls = %v, want none on a -migrate refusal", calls)
			}
		})
	}
}

// The same -migrate path with a valid registry must reach the driver, so the
// zero above is a refusal and not a path that never touches the database.
func TestMigratePositiveControlReachesDatabase(t *testing.T) {
	sqlDB, log := newRecordingDB(t)

	withArgs(t, []string{"tango", "-migrate"}, func() {
		handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, db.SQLite, validMigrations())
		if !handled {
			t.Fatal("handled = false, want true")
		}
		if err != nil {
			t.Fatalf("DispatchFlags on a valid registry: %v", err)
		}
	})
	if !log.has("connect") || len(log.snapshot()) == 0 {
		t.Fatalf("calls = %v, want database calls for a valid -migrate", log.snapshot())
	}
}

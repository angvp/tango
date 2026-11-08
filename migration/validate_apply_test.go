package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/testdb"
)

func conflictingRegistry() []Migration {
	return []Migration{
		{App: "blog", Name: "0005_add_email", Reversible: true,
			Up:   []Step{CreateTable{Table: "vc_email", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []Step{DropTable{Table: "vc_email"}}},
		{App: "blog", Name: "0005_add_phone", Reversible: true,
			Up:   []Step{CreateTable{Table: "vc_phone", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []Step{DropTable{Table: "vc_phone"}}},
	}
}

func TestApplyPendingRejectsConflictingRegistryWithZeroDatabaseEffects(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	err := ApplyPending(ctx, sqlDB, dialect, conflictingRegistry())
	if !errors.Is(err, ErrConflictingMigrations) {
		t.Fatalf("ApplyPending error = %v, want ErrConflictingMigrations", err)
	}
	for _, table := range []string{"tango_migrations", "vc_email", "vc_phone"} {
		if tableExists(t, sqlDB, dialect, table) {
			t.Fatalf("table %q exists after a rejected ApplyPending; want no database effects", table)
		}
	}
}

func TestApplyPendingRejectsConflictWhenOneMigrationIsAlreadyApplied(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	registry := conflictingRegistry()

	if err := ApplyPending(ctx, sqlDB, dialect, registry[:1]); err != nil {
		t.Fatalf("seeding ApplyPending with the first migration: %v", err)
	}

	err := ApplyPending(ctx, sqlDB, dialect, registry)
	if !errors.Is(err, ErrConflictingMigrations) {
		t.Fatalf("ApplyPending error = %v, want ErrConflictingMigrations", err)
	}
	if tableExists(t, sqlDB, dialect, "vc_phone") {
		t.Fatal("vc_phone exists after a rejected ApplyPending; want no database effects")
	}
	applied, err := AppliedMigrations(ctx, sqlDB)
	if err != nil {
		t.Fatalf("AppliedMigrations: %v", err)
	}
	if len(applied) != 1 || !applied[MigrationKey{App: "blog", Name: "0005_add_email"}] {
		t.Fatalf("applied = %v, want only blog/0005_add_email", applied)
	}
}

// ApplyPending must refuse a conflicting registry before any database call,
// so a closed handle surfaces the conflict rather than a database error.
func TestApplyPendingValidatesBeforeTouchingClosedDatabase(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	sqlDB.Close()

	err := ApplyPending(context.Background(), sqlDB, dialect, conflictingRegistry())
	if !errors.Is(err, ErrConflictingMigrations) {
		t.Fatalf("ApplyPending error = %v, want ErrConflictingMigrations with a closed database", err)
	}
}

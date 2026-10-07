package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/testdb"
)

func TestRollbackLastReversesSchemaAndRemovesTrackingRow(t *testing.T) {
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

	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("RollbackLast returned error: %v", err)
	}

	if tableExists(t, sqlDB, dialect, "account") {
		t.Fatalf("table %q still exists after rollback", "account")
	}
	if countTrackingRows(t, sqlDB) != 0 {
		t.Fatalf("tango_migrations has %d rows after rollback, want 0", countTrackingRows(t, sqlDB))
	}
}

func TestRollbackLastRejectsIrreversibleMigration(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_create_account", App: "users", Reversible: true, Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "legacy", Type: "text"}}},
		}, Down: []Step{
			DropTable{Table: "account"},
		}},
		{Name: "0002_drop_legacy", App: "users", Reversible: false, Up: []Step{
			DropColumn{Table: "account", Column: "legacy"},
		}},
	}

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	err := RollbackLast(ctx, sqlDB, dialect, migrations)
	if !errors.Is(err, ErrIrreversibleMigration) {
		t.Fatalf("error = %v, want it to wrap ErrIrreversibleMigration", err)
	}

	if !tableExists(t, sqlDB, dialect, "account") {
		t.Fatalf("table %q should still exist after a rejected rollback", "account")
	}
	if countTrackingRows(t, sqlDB) != 2 {
		t.Fatalf("tango_migrations has %d rows after a rejected rollback, want 2 (unchanged)", countTrackingRows(t, sqlDB))
	}
}

func TestRollbackLastWithNothingAppliedReturnsClearError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	err := RollbackLast(ctx, sqlDB, dialect, nil)
	if !errors.Is(err, ErrNoAppliedMigrations) {
		t.Fatalf("error = %v, want it to wrap ErrNoAppliedMigrations", err)
	}
}

func TestRollbackLastTwiceInARowReturnsClearErrorOnSecondCall(t *testing.T) {
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
	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("first RollbackLast returned error: %v", err)
	}

	err := RollbackLast(ctx, sqlDB, dialect, migrations)
	if !errors.Is(err, ErrNoAppliedMigrations) {
		t.Fatalf("second RollbackLast error = %v, want it to wrap ErrNoAppliedMigrations", err)
	}
}

func TestRollbackLastUsesAppAndNameAsIdentity(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_auto", App: "posts", Reversible: true, Up: []Step{
			CreateTable{Table: "post", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}, Down: []Step{
			DropTable{Table: "post"},
		}},
		{Name: "0001_auto", App: "users", Reversible: true, Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}, Down: []Step{
			DropTable{Table: "account"},
		}},
	}

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("RollbackLast returned error: %v", err)
	}

	if !tableExists(t, sqlDB, dialect, "post") {
		t.Fatalf("post table was rolled back, want only users/0001_auto rolled back")
	}
	if tableExists(t, sqlDB, dialect, "account") {
		t.Fatalf("account table still exists after users/0001_auto rollback")
	}
	if countTrackingRows(t, sqlDB) != 1 {
		t.Fatalf("tango_migrations has %d rows after rollback, want 1", countTrackingRows(t, sqlDB))
	}
}

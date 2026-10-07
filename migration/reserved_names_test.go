package migration

import (
	"context"
	"slices"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// TestEveryStepAppliesAndRollsBackOnReservedWordNames runs every Step kind,
// forward through ApplyPending and back through RollbackLast, against
// tables and columns named after SQL reserved words ("user", "order",
// "group", "select", "from"), so each statement ApplyStep generates must
// quote its identifiers on both dialects.
func TestEveryStepAppliesAndRollsBackOnReservedWordNames(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect := testdb.Open(t)

	createTables := Migration{
		App: "shop", Name: "0001_initial", Reversible: true,
		Up: []Step{
			CreateTable{Table: "user", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "order", Type: "integer"},
				{Name: "group", Type: "text", Unique: true},
				{Name: "select", Type: "text", Indexed: true},
			}},
			CreateTable{Table: "order", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "user", Type: "integer", References: "user"},
			}},
		},
		Down: []Step{DropTable{Table: "order"}, DropTable{Table: "user"}},
	}
	alterColumns := Migration{
		App: "shop", Name: "0002_from", Reversible: true,
		Up: []Step{
			AddColumn{Table: "user", Column: Column{Name: "from", Type: "text", Default: "'x'"}},
			CreateIndex{Table: "user", Column: "from"},
			AlterColumnUnique{Table: "user", Column: "order", Unique: true},
		},
		Down: []Step{
			AlterColumnUnique{Table: "user", Column: "order", Unique: false},
			DropIndex{Table: "user", Column: "from"},
			DropColumn{Table: "user", Column: "from"},
		},
	}
	migrations := []Migration{createTables, alterColumns}

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	if got := columnNames(t, sqlDB, dialect, "user"); !slices.Equal(got, []string{"id", "order", "group", "select", "from"}) {
		t.Fatalf("user columns after ApplyPending = %v", got)
	}
	if !tableExists(t, sqlDB, dialect, "order") {
		t.Fatal("table order missing after ApplyPending")
	}

	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("RollbackLast 0002: %v", err)
	}
	if got := columnNames(t, sqlDB, dialect, "user"); !slices.Equal(got, []string{"id", "order", "group", "select"}) {
		t.Fatalf("user columns after rolling back 0002 = %v", got)
	}

	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("RollbackLast 0001: %v", err)
	}
	if tableExists(t, sqlDB, dialect, "user") || tableExists(t, sqlDB, dialect, "order") {
		t.Fatal("tables user/order still exist after rolling back 0001")
	}
}

// TestQuotedStepsOperateOnTablesCreatedUnquoted checks a database migrated
// before identifiers were quoted keeps working: tables, columns and indexes
// created by unquoted DDL are found by the quoted DDL of later migrations,
// so quoting changes no identifier's identity and forces no re-migration.
func TestQuotedStepsOperateOnTablesCreatedUnquoted(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect := testdb.Open(t)
	idType := "INTEGER PRIMARY KEY AUTOINCREMENT"
	if dialect == db.Postgres {
		idType = "BIGSERIAL PRIMARY KEY"
	}
	for _, statement := range []string{
		"CREATE TABLE legacy_widget (id " + idType + ", name TEXT, slug TEXT)",
		"CREATE INDEX idx_legacy_widget_name ON legacy_widget (name)",
		"CREATE UNIQUE INDEX uniq_legacy_widget_slug ON legacy_widget (slug)",
	} {
		if _, err := sqlDB.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	for _, step := range []Step{
		AddColumn{Table: "legacy_widget", Column: Column{Name: "color", Type: "text"}},
		DropIndex{Table: "legacy_widget", Column: "name"},
		AlterColumnUnique{Table: "legacy_widget", Column: "slug", Unique: false},
		DropColumn{Table: "legacy_widget", Column: "name"},
		CreateIndex{Table: "legacy_widget", Column: "color"},
	} {
		if err := ApplyStep(ctx, sqlDB, dialect, step); err != nil {
			t.Fatalf("ApplyStep(%#v) on an unquoted legacy table: %v", step, err)
		}
	}
	if got := columnNames(t, sqlDB, dialect, "legacy_widget"); !slices.Equal(got, []string{"id", "slug", "color"}) {
		t.Fatalf("legacy_widget columns = %v, want [id slug color]", got)
	}
	if err := ApplyStep(ctx, sqlDB, dialect, DropTable{Table: "legacy_widget"}); err != nil {
		t.Fatalf("DropTable on an unquoted legacy table: %v", err)
	}
}

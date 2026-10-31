package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

// Altering a column the table does not have is refused, and the rows are
// left as they were. The error text differs by dialect, so only the refusal
// and the unchanged row are asserted.
func TestAlterColumnTypeRefusesMissingColumn(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "sample", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "v", Type: "integer"},
		}},
	)
	mustExec(t, sqlDB, "INSERT INTO sample (id, v) VALUES (1, 7)")

	err := ApplyStep(context.Background(), sqlDB, dialect, AlterColumnType{Table: "sample", Column: "missing", From: "integer", To: "real"})
	if err == nil {
		t.Fatal("AlterColumnType on a missing column succeeded, want it refused")
	}
	if got := readColumn(t, sqlDB, "v", int64(0)); len(got) != 1 {
		t.Fatalf("sample rows after refused change = %d, want 1", len(got))
	}
}

// On SQLite a real-to-text change rebuilds the table and rewrites each row by
// its primary key, so a table without one is refused before any row changes.
func TestAlterRealToTextNeedsPrimaryKeyOnSQLite(t *testing.T) {
	testdb.SQLiteOnly(t, "the real-to-text rebuild re-keys rows by primary key and runs only on SQLite; PostgreSQL converts in one ALTER")
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "sample", Columns: []Column{{Name: "v", Type: "real"}}},
	)
	mustExec(t, sqlDB, "INSERT INTO sample (v) VALUES (1.5)")

	err := ApplyStep(context.Background(), sqlDB, dialect, AlterColumnType{Table: "sample", Column: "v", From: "real", To: "text"})
	if err == nil || !strings.Contains(err.Error(), "needs a primary key") {
		t.Fatalf("AlterColumnType real to text without a primary key: err = %v, want a primary key refusal", err)
	}
	if got := columnType(t, sqlDB, dialect, "sample", "v"); got != "real" {
		t.Fatalf("v type after refused change = %q, want %q", got, "real")
	}
}

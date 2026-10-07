package migration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// The foreign-key tests below assert the REFERENCES clause through its
// effect: the database rejects a dangling value. testdb turns SQLite's
// foreign-key pragma on (as db.ParseDSN does for apps), and PostgreSQL
// enforces foreign keys natively, so the same assertion holds on both.

func TestApplyStepCreateTableWithForeignKeyEnforcesReferences(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	mustApply(t, sqlDB, dialect, CreateTable{Table: "author", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
	}})
	mustApply(t, sqlDB, dialect, CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "author"},
	}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO post (author_id) VALUES (999)"); err == nil {
		t.Fatal("insert with dangling foreign key succeeded, want a constraint violation")
	}
}

func TestApplyStepAddColumnWithForeignKeyEnforcesReferences(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	mustApply(t, sqlDB, dialect, CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	mustApply(t, sqlDB, dialect, CreateTable{Table: "post", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	mustApply(t, sqlDB, dialect, AddColumn{Table: "post", Column: Column{Name: "author_id", Type: "integer", References: "author"}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO post (author_id) VALUES (999)"); err == nil {
		t.Fatal("insert with dangling foreign key into an added column succeeded, want a constraint violation")
	}
}

func TestSQLiteForeignKeyConstraintIsNotEnforcedWithoutPragmaDSN(t *testing.T) {
	// Documents the baseline the tests above rely on: without the pragma
	// DSN, SQLite accepts a dangling foreign key value — proving the pragma
	// testdb applies is actually doing something, not passing vacuously.
	// testdb always turns the pragma on, so this opens its own database.
	testdb.SQLiteOnly(t, "documents SQLite's default of not enforcing foreign keys, which PostgreSQL has no equivalent of; needs a database opened without testdb's foreign-key pragma")
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()

	mustApply(t, sqlDB, db.SQLite, CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	mustApply(t, sqlDB, db.SQLite, CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "author"},
	}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO post (author_id) VALUES (999)"); err != nil {
		t.Fatalf("insert with dangling foreign key failed without the pragma: %v", err)
	}
}

package migration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
)

func TestApplyStepCreateTableWithForeignKeyEmitsReferencesClause(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()

	mustApply(t, sqlDB, CreateTable{Table: "author", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
	}})

	step := CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "author"},
	}}
	if err := ApplyStep(ctx, sqlDB, db.SQLite, step); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}

	var schema string
	if err := sqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='post'").Scan(&schema); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if !strings.Contains(schema, "REFERENCES author") {
		t.Fatalf("post table schema = %q, want it to contain %q", schema, "REFERENCES author")
	}
}

func TestApplyStepAddColumnWithForeignKeyEmitsReferencesClause(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()

	mustApply(t, sqlDB, CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	mustApply(t, sqlDB, CreateTable{Table: "post", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})

	step := AddColumn{Table: "post", Column: Column{Name: "author_id", Type: "integer", References: "author"}}
	if err := ApplyStep(ctx, sqlDB, db.SQLite, step); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}

	var schema string
	if err := sqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='post'").Scan(&schema); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if !strings.Contains(schema, "REFERENCES author") {
		t.Fatalf("post table schema = %q, want it to contain %q", schema, "REFERENCES author")
	}
}

func TestSQLiteForeignKeyConstraintIsEnforcedWithPragmaDSN(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", db.SQLiteForeignKeysDSN(":memory:"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()

	mustApply(t, sqlDB, CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	mustApply(t, sqlDB, CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "author"},
	}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO post (author_id) VALUES (999)"); err == nil {
		t.Fatal("insert with dangling foreign key succeeded, want a constraint violation")
	}
}

func TestApplyStepCreateTableWithForeignKeyPostgresEnforcesConstraint(t *testing.T) {
	sqlDB := openPostgresDDLTestDB(t)
	ctx := context.Background()

	mustApply(t, sqlDB, CreateTable{Table: "fk_ddl_author", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
	}})
	mustApply(t, sqlDB, CreateTable{Table: "fk_ddl_post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "fk_ddl_author"},
	}})
	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS fk_ddl_post")
		_, _ = sqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS fk_ddl_author")
	})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO fk_ddl_post (author_id) VALUES (999)"); err == nil {
		t.Fatal("insert with dangling foreign key succeeded on Postgres, want a constraint violation")
	}
}

func TestSQLiteForeignKeyConstraintIsNotEnforcedWithoutPragmaDSN(t *testing.T) {
	// Documents the baseline this ticket changes: without the pragma DSN,
	// SQLite accepts a dangling foreign key value — proving the pragma in
	// the test above is actually doing something, not passing vacuously.
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()

	mustApply(t, sqlDB, CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	mustApply(t, sqlDB, CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "author"},
	}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO post (author_id) VALUES (999)"); err != nil {
		t.Fatalf("insert with dangling foreign key failed without the pragma: %v", err)
	}
}

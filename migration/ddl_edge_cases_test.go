package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
)

// TestBaseTypeSQLAcrossDialects is a direct, pure string-generation table
// for baseTypeSQL's full dialect matrix — the same "assert the SQL string
// for db.Postgres without a live Postgres connection" pattern already used
// by TestAddColumnDefSQLDefaultAcrossDialects and TestColumnDefSQLIgnoresDefault.
func TestBaseTypeSQLAcrossDialects(t *testing.T) {
	cases := []struct {
		columnType   string
		wantSQLite   string
		wantPostgres string
	}{
		{columnType: "text", wantSQLite: "TEXT", wantPostgres: "TEXT"},
		{columnType: "boolean", wantSQLite: "BOOLEAN", wantPostgres: "BOOLEAN"},
		{columnType: "real", wantSQLite: "REAL", wantPostgres: "DOUBLE PRECISION"},
		{columnType: "timestamp", wantSQLite: "TIMESTAMP", wantPostgres: "TIMESTAMPTZ"},
		{columnType: "integer", wantSQLite: "INTEGER", wantPostgres: "BIGINT"},
		{columnType: "some-unknown-type", wantSQLite: "TEXT", wantPostgres: "TEXT"},
	}

	for _, tc := range cases {
		t.Run(tc.columnType, func(t *testing.T) {
			if got := baseTypeSQL(db.SQLite, tc.columnType); got != tc.wantSQLite {
				t.Errorf("baseTypeSQL(SQLite, %q) = %q, want %q", tc.columnType, got, tc.wantSQLite)
			}
			if got := baseTypeSQL(db.Postgres, tc.columnType); got != tc.wantPostgres {
				t.Errorf("baseTypeSQL(Postgres, %q) = %q, want %q", tc.columnType, got, tc.wantPostgres)
			}
		})
	}
}

// TestColumnDefSQLPrimaryKeyBranches directly asserts columnDefSQL's SQL
// text for both the integer-primary-key fast path (BIGSERIAL on Postgres,
// AUTOINCREMENT on SQLite) and a non-integer primary key column, again as a
// plain string assertion requiring no live Postgres connection.
func TestColumnDefSQLPrimaryKeyBranches(t *testing.T) {
	integerPK := Column{Name: "id", Type: "integer", PrimaryKey: true}
	if got := columnDefSQL(db.SQLite, integerPK); got != "id INTEGER PRIMARY KEY AUTOINCREMENT" {
		t.Errorf("columnDefSQL(SQLite, integer PK) = %q", got)
	}
	if got := columnDefSQL(db.Postgres, integerPK); got != "id BIGSERIAL PRIMARY KEY" {
		t.Errorf("columnDefSQL(Postgres, integer PK) = %q", got)
	}

	textPK := Column{Name: "slug", Type: "text", PrimaryKey: true}
	if got := columnDefSQL(db.SQLite, textPK); got != "slug TEXT PRIMARY KEY" {
		t.Errorf("columnDefSQL(SQLite, text PK) = %q, want %q", got, "slug TEXT PRIMARY KEY")
	}
	if got := columnDefSQL(db.Postgres, textPK); got != "slug TEXT PRIMARY KEY" {
		t.Errorf("columnDefSQL(Postgres, text PK) = %q, want %q", got, "slug TEXT PRIMARY KEY")
	}

	referencing := Column{Name: "author_id", Type: "integer", PrimaryKey: true, References: "author"}
	if got := columnDefSQL(db.Postgres, referencing); !strings.Contains(got, "REFERENCES author") {
		t.Errorf("columnDefSQL with References = %q, want it to contain REFERENCES author", got)
	}
}

// TestCreateTableSQLGeneratesUniqueAndIndexStatements confirms
// createTableSQL emits a CREATE UNIQUE INDEX / CREATE INDEX statement per
// Unique/Indexed column, in addition to the base CREATE TABLE, and that
// ApplyStep actually enforces them.
func TestCreateTableSQLGeneratesUniqueAndIndexStatements(t *testing.T) {
	step := CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "slug", Type: "text", Unique: true},
		{Name: "category", Type: "text", Indexed: true},
	}}

	statements := createTableSQL(db.SQLite, step)
	if len(statements) != 3 {
		t.Fatalf("createTableSQL returned %d statements, want 3 (create table + unique index + index): %v", len(statements), statements)
	}
	if !strings.Contains(statements[1], "CREATE UNIQUE INDEX uniq_widget_slug ON widget (slug)") {
		t.Errorf("statements[1] = %q, want a unique index on slug", statements[1])
	}
	if !strings.Contains(statements[2], "CREATE INDEX idx_widget_category ON widget (category)") {
		t.Errorf("statements[2] = %q, want a plain index on category", statements[2])
	}

	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	if err := ApplyStep(ctx, sqlDB, db.SQLite, step); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug, category) VALUES ('a', 'x')"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug, category) VALUES ('a', 'y')"); err == nil {
		t.Fatal("expected unique constraint violation on duplicate slug from CreateTable's Unique column")
	}
}

// TestApplyStepUnsupportedStepTypeReturnsError covers ApplyStep's default
// case for a Step implementation it doesn't know how to translate.
type unknownStep struct{}

func (unknownStep) isMigrationStep() {}

func TestApplyStepUnsupportedStepTypeReturnsError(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	err := ApplyStep(context.Background(), sqlDB, db.SQLite, unknownStep{})
	if err == nil {
		t.Fatal("ApplyStep returned nil error for an unsupported step type, want an error")
	}
}

// TestApplyStepDropColumnOnPostgresDialectExecutesDirectly exercises
// ApplyStep's Postgres-dialect DropColumn branch (a direct ALTER TABLE ...
// DROP COLUMN, unlike SQLite's rebuild-the-table fallback) against the
// existing SQLite-backed test setup: modernc.org/sqlite supports the same
// DROP COLUMN syntax Postgres does, so this proves the dialect branch is
// reached and produces working DDL without needing a live Postgres
// connection — the live end-to-end Postgres path is already covered
// separately by TestApplyStepFullLifecyclePostgres.
func TestApplyStepDropColumnOnPostgresDialectExecutesDirectly(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "legacy", Type: "text"},
	}})

	if err := ApplyStep(ctx, sqlDB, db.Postgres, DropColumn{Table: "widget", Column: "legacy"}); err != nil {
		t.Fatalf("ApplyStep(DropColumn, db.Postgres) returned error: %v", err)
	}

	names := columnNames(t, sqlDB, "widget")
	if contains(names, "legacy") {
		t.Fatalf("columns = %v, want %q dropped", names, "legacy")
	}
}

// TestApplyStepDropColumnOnMissingColumnReturnsError covers
// rebuildTableDroppingColumn's "column does not exist" guard.
func TestApplyStepDropColumnOnMissingColumnReturnsError(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
	}})

	err := ApplyStep(context.Background(), sqlDB, db.SQLite, DropColumn{Table: "widget", Column: "no_such_column"})
	if err == nil {
		t.Fatal("ApplyStep(DropColumn) for a nonexistent column returned nil error, want an error")
	}
}

// TestApplyStepDropColumnSurfacesClosedConnectionError forces
// sqliteTableColumns' PRAGMA query to fail (a closed connection stands in
// for any ordinary driver-level failure) and confirms rebuildTableDroppingColumn
// propagates it instead of panicking or silently doing nothing.
func TestApplyStepDropColumnSurfacesClosedConnectionError(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "legacy", Type: "text"},
	}})
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err := ApplyStep(context.Background(), sqlDB, db.SQLite, DropColumn{Table: "widget", Column: "legacy"})
	if err == nil {
		t.Fatal("ApplyStep(DropColumn) on a closed connection returned nil error, want an error")
	}
}

// TestExecAllStopsAtFirstFailingStatement covers execAll's error return: a
// later statement never runs once an earlier one fails.
func TestExecAllStopsAtFirstFailingStatement(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()

	err := execAll(ctx, sqlDB, []string{
		"CREATE TABLE ok_table (id INTEGER PRIMARY KEY)",
		"THIS IS NOT VALID SQL",
		"CREATE TABLE never_created (id INTEGER PRIMARY KEY)",
	})
	if err == nil {
		t.Fatal("execAll returned nil error for an invalid statement, want an error")
	}
	if tableExists(t, sqlDB, "never_created") {
		t.Fatal("execAll ran a statement after an earlier one failed")
	}
}

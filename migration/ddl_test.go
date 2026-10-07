package migration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// tableExists reports whether table exists in sqlDB, asking the dialect's
// own catalog: sqlite_master on SQLite, information_schema in the test's
// schema (the connection's current_schema) on PostgreSQL.
func tableExists(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table string) bool {
	t.Helper()
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name=?"
	if dialect == db.Postgres {
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1"
	}
	var name string
	err := sqlDB.QueryRow(query, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("look up table %q: %v", table, err)
	}
	return true
}

// columnNames lists table's columns in declaration order, through PRAGMA
// table_info on SQLite and information_schema on PostgreSQL.
func columnNames(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table string) []string {
	t.Helper()
	query := "SELECT name FROM pragma_table_info(?) ORDER BY cid"
	if dialect == db.Postgres {
		query = "SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 ORDER BY ordinal_position"
	}
	rows, err := sqlDB.Query(query, table)
	if err != nil {
		t.Fatalf("list columns of %q: %v", table, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list columns of %q: %v", table, err)
	}
	return names
}

// TestApplyStepBasicOperations merges TestApplyStepCreateTable,
// TestApplyStepDropTable and TestApplyStepAddColumn into one table.
func TestApplyStepBasicOperations(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect)
		step  Step
		check func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect)
	}{
		{
			name: "create table creates a new table",
			step: CreateTable{Table: "widget", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "name", Type: "text"},
			}},
			check: func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) {
				if !tableExists(t, sqlDB, dialect, "widget") {
					t.Errorf("table %q was not created", "widget")
				}
			},
		},
		{
			name: "drop table removes an existing table",
			setup: func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) {
				mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
			},
			step: DropTable{Table: "widget"},
			check: func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) {
				if tableExists(t, sqlDB, dialect, "widget") {
					t.Errorf("table %q still exists after DropTable", "widget")
				}
			},
		},
		{
			name: "add column adds a new column to an existing table",
			setup: func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) {
				mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
			},
			step: AddColumn{Table: "widget", Column: Column{Name: "name", Type: "text"}},
			check: func(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) {
				names := columnNames(t, sqlDB, dialect, "widget")
				if !contains(names, "name") {
					t.Errorf("columns = %v, want to contain %q", names, "name")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, dialect := testdb.Open(t)
			if tc.setup != nil {
				tc.setup(t, sqlDB, dialect)
			}

			if err := ApplyStep(context.Background(), sqlDB, dialect, tc.step); err != nil {
				t.Fatalf("case %q: ApplyStep returned error: %v", tc.name, err)
			}

			tc.check(t, sqlDB, dialect)
		})
	}
}

func TestApplyStepAddColumnWithDefaultBackfillsExistingRows(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	if _, err := sqlDB.Exec("INSERT INTO widget (id) VALUES (1)"); err != nil {
		t.Fatalf("seed existing row: %v", err)
	}

	mustApply(t, sqlDB, dialect, AddColumn{Table: "widget", Column: Column{Name: "is_staff", Type: "boolean", Default: "TRUE"}})

	var isStaff bool
	if err := sqlDB.QueryRowContext(ctx, "SELECT is_staff FROM widget WHERE id = 1").Scan(&isStaff); err != nil {
		t.Fatalf("query backfilled column: %v", err)
	}
	if !isStaff {
		t.Fatal("existing row's is_staff = false, want true (backfilled from Default)")
	}

	if _, err := sqlDB.Exec("INSERT INTO widget (id) VALUES (2)"); err != nil {
		t.Fatalf("insert new row without specifying is_staff: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, "SELECT is_staff FROM widget WHERE id = 2").Scan(&isStaff); err != nil {
		t.Fatalf("query new row's default column: %v", err)
	}
	if !isStaff {
		t.Fatal("new row's is_staff = false, want true (DEFAULT applies to new inserts too)")
	}
}

// TestAddColumnDefSQLDefaultAcrossDialects checks Column.Default's generated
// DDL directly against both dialects' addColumnDefSQL branch, without
// needing a live Postgres connection. "TRUE"/"FALSE" are the
// only literals this milestone uses, and both SQLite and Postgres accept
// them for a boolean column — a bare "1"/"0" literal, by contrast, is valid
// SQLite but rejected by Postgres for a BOOLEAN column, which is exactly
// the portability trap this milestone's Default literals must avoid.
func TestAddColumnDefSQLDefaultAcrossDialects(t *testing.T) {
	column := Column{Name: "is_staff", Type: "boolean", Default: "TRUE"}

	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		got := addColumnDefSQL(dialect, column)
		if !strings.Contains(got, "NOT NULL DEFAULT TRUE") {
			t.Fatalf("addColumnDefSQL(%v, ...) = %q, want it to contain %q", dialect, got, "NOT NULL DEFAULT TRUE")
		}
	}

	withoutDefault := addColumnDefSQL(db.SQLite, Column{Name: "name", Type: "text"})
	if strings.Contains(withoutDefault, "DEFAULT") {
		t.Fatalf("addColumnDefSQL with no Default = %q, want no DEFAULT clause", withoutDefault)
	}
}

// TestColumnDefSQLIgnoresDefault confirms CreateTable's column generator
// (columnDefSQL) never emits a DEFAULT clause, even when Column.Default is
// set — Default is an AddColumn-only backfill escape hatch (see
// addColumnDefSQL and columnDefSQL's doc comment), not a general
// default-value system that would also apply to a freshly created table.
func TestColumnDefSQLIgnoresDefault(t *testing.T) {
	column := Column{Name: "is_staff", Type: "boolean", Default: "TRUE"}

	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		got := columnDefSQL(dialect, column)
		if strings.Contains(got, "DEFAULT") {
			t.Fatalf("columnDefSQL(%v, ...) = %q, want no DEFAULT clause even with Column.Default set", dialect, got)
		}
	}
}

// TestApplyStepCreateTableIgnoresColumnDefault confirms the same at the
// CreateTable step level: a freshly created table's column, even with
// Default set, has no DEFAULT/NOT NULL constraint and accepts a NULL insert
// for it — proving Default has no effect outside AddColumn.
func TestApplyStepCreateTableIgnoresColumnDefault(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "is_staff", Type: "boolean", Default: "TRUE"},
	}})

	if _, err := sqlDB.Exec("INSERT INTO widget (id, is_staff) VALUES (1, NULL)"); err != nil {
		t.Fatalf("insert explicit NULL into a Default-carrying CreateTable column: %v", err)
	}

	var isStaff sql.NullBool
	if err := sqlDB.QueryRow("SELECT is_staff FROM widget WHERE id = 1").Scan(&isStaff); err != nil {
		t.Fatalf("query column: %v", err)
	}
	if isStaff.Valid {
		t.Fatalf("is_staff = %v, want NULL (CreateTable must ignore Column.Default)", isStaff)
	}
}

// TestApplyStepDropColumnRemovesColumnAndPreservesData covers SQLite's
// table rebuild and PostgreSQL's direct ALTER TABLE ... DROP COLUMN alike.
func TestApplyStepDropColumnRemovesColumnAndPreservesData(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "name", Type: "text"},
		{Name: "legacy", Type: "text"},
	}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (name, legacy) VALUES ('alpha', 'junk')"); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	if err := ApplyStep(ctx, sqlDB, dialect, DropColumn{Table: "widget", Column: "legacy"}); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}

	names := columnNames(t, sqlDB, dialect, "widget")
	if contains(names, "legacy") {
		t.Fatalf("columns = %v, want %q dropped", names, "legacy")
	}

	var name string
	if err := sqlDB.QueryRow("SELECT name FROM widget").Scan(&name); err != nil {
		t.Fatalf("select after rebuild: %v", err)
	}
	if name != "alpha" {
		t.Fatalf("name = %q, want %q (data lost during rebuild)", name, "alpha")
	}
}

func TestApplyStepAlterColumnUniqueRoundTrips(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "slug", Type: "text"},
	}})

	if err := ApplyStep(ctx, sqlDB, dialect, AlterColumnUnique{Table: "widget", Column: "slug", Unique: true}); err != nil {
		t.Fatalf("ApplyStep (add unique) returned error: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug) VALUES ('a')"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug) VALUES ('a')"); err == nil {
		t.Fatalf("expected unique constraint violation on duplicate slug")
	}

	if err := ApplyStep(ctx, sqlDB, dialect, AlterColumnUnique{Table: "widget", Column: "slug", Unique: false}); err != nil {
		t.Fatalf("ApplyStep (drop unique) returned error: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug) VALUES ('a')"); err != nil {
		t.Fatalf("insert after dropping unique constraint failed: %v", err)
	}
}

func TestApplyStepCreateAndDropIndex(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	mustApply(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "category", Type: "text"},
	}})

	if err := ApplyStep(ctx, sqlDB, dialect, CreateIndex{Table: "widget", Column: "category"}); err != nil {
		t.Fatalf("ApplyStep (CreateIndex) returned error: %v", err)
	}
	if err := ApplyStep(ctx, sqlDB, dialect, DropIndex{Table: "widget", Column: "category"}); err != nil {
		t.Fatalf("ApplyStep (DropIndex) returned error: %v", err)
	}
}

func mustApply(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, step Step) {
	t.Helper()
	if err := ApplyStep(context.Background(), sqlDB, dialect, step); err != nil {
		t.Fatalf("ApplyStep(%T) returned error: %v", step, err)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// TestApplyStepFullLifecycle runs every step kind in sequence against one
// table, the shape a model's migrations take over its lifetime.
func TestApplyStepFullLifecycle(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	steps := []Step{
		CreateTable{Table: "ddl_widget", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "name", Type: "text"},
			{Name: "legacy", Type: "text"},
		}},
		AddColumn{Table: "ddl_widget", Column: Column{Name: "category", Type: "text"}},
		AlterColumnUnique{Table: "ddl_widget", Column: "name", Unique: true},
		CreateIndex{Table: "ddl_widget", Column: "category"},
		DropIndex{Table: "ddl_widget", Column: "category"},
		AlterColumnUnique{Table: "ddl_widget", Column: "name", Unique: false},
		DropColumn{Table: "ddl_widget", Column: "legacy"},
	}

	for _, step := range steps {
		if err := ApplyStep(ctx, sqlDB, dialect, step); err != nil {
			t.Fatalf("ApplyStep(%T) returned error: %v", step, err)
		}
	}

	if got, want := columnNames(t, sqlDB, dialect, "ddl_widget"), []string{"id", "name", "category"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("columns after lifecycle = %v, want %v", got, want)
	}
}

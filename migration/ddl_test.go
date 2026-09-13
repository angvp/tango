package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/angvp/tango/db"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func openDDLTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

func tableExists(t *testing.T, sqlDB *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := sqlDB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return true
}

func columnNames(t *testing.T, sqlDB *sql.DB, table string) []string {
	t.Helper()
	rows, err := sqlDB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var cid int
		var name, declType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &declType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		names = append(names, name)
	}
	return names
}

func TestApplyStepCreateTable(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	step := CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "name", Type: "text"},
	}}

	if err := ApplyStep(context.Background(), sqlDB, db.SQLite, step); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}

	if !tableExists(t, sqlDB, "widget") {
		t.Fatalf("table %q was not created", "widget")
	}
}

func TestApplyStepDropTable(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})

	if err := ApplyStep(ctx, sqlDB, db.SQLite, DropTable{Table: "widget"}); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}
	if tableExists(t, sqlDB, "widget") {
		t.Fatalf("table %q still exists after DropTable", "widget")
	}
}

func TestApplyStepAddColumn(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})

	mustApply(t, sqlDB, AddColumn{Table: "widget", Column: Column{Name: "name", Type: "text"}})

	names := columnNames(t, sqlDB, "widget")
	if !contains(names, "name") {
		t.Fatalf("columns = %v, want to contain %q", names, "name")
	}
}

func TestApplyStepAddColumnWithDefaultBackfillsExistingRows(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	if _, err := sqlDB.Exec("INSERT INTO widget (id) VALUES (1)"); err != nil {
		t.Fatalf("seed existing row: %v", err)
	}

	mustApply(t, sqlDB, AddColumn{Table: "widget", Column: Column{Name: "is_staff", Type: "boolean", Default: "TRUE"}})

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

func TestApplyStepDropColumnRebuildsTableAndPreservesData(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "name", Type: "text"},
		{Name: "legacy", Type: "text"},
	}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (name, legacy) VALUES ('alpha', 'junk')"); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	if err := ApplyStep(ctx, sqlDB, db.SQLite, DropColumn{Table: "widget", Column: "legacy"}); err != nil {
		t.Fatalf("ApplyStep returned error: %v", err)
	}

	names := columnNames(t, sqlDB, "widget")
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
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "slug", Type: "text"},
	}})

	if err := ApplyStep(ctx, sqlDB, db.SQLite, AlterColumnUnique{Table: "widget", Column: "slug", Unique: true}); err != nil {
		t.Fatalf("ApplyStep (add unique) returned error: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug) VALUES ('a')"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug) VALUES ('a')"); err == nil {
		t.Fatalf("expected unique constraint violation on duplicate slug")
	}

	if err := ApplyStep(ctx, sqlDB, db.SQLite, AlterColumnUnique{Table: "widget", Column: "slug", Unique: false}); err != nil {
		t.Fatalf("ApplyStep (drop unique) returned error: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO widget (slug) VALUES ('a')"); err != nil {
		t.Fatalf("insert after dropping unique constraint failed: %v", err)
	}
}

func TestApplyStepCreateAndDropIndex(t *testing.T) {
	sqlDB := openDDLTestDB(t)
	ctx := context.Background()
	mustApply(t, sqlDB, CreateTable{Table: "widget", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "category", Type: "text"},
	}})

	if err := ApplyStep(ctx, sqlDB, db.SQLite, CreateIndex{Table: "widget", Column: "category"}); err != nil {
		t.Fatalf("ApplyStep (CreateIndex) returned error: %v", err)
	}
	if err := ApplyStep(ctx, sqlDB, db.SQLite, DropIndex{Table: "widget", Column: "category"}); err != nil {
		t.Fatalf("ApplyStep (DropIndex) returned error: %v", err)
	}
}

func mustApply(t *testing.T, sqlDB *sql.DB, step Step) {
	t.Helper()
	if err := ApplyStep(context.Background(), sqlDB, db.SQLite, step); err != nil {
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

func postgresDDLTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TANGO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TANGO_TEST_POSTGRES_DSN not set; skipping Postgres-backed migration DDL tests")
	}
	return dsn
}

func openPostgresDDLTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("pgx", postgresDDLTestDSN(t))
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	_, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS ddl_widget")
	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS ddl_widget")
	})
	return sqlDB
}

func TestApplyStepFullLifecyclePostgres(t *testing.T) {
	sqlDB := openPostgresDDLTestDB(t)
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
		if err := ApplyStep(ctx, sqlDB, db.Postgres, step); err != nil {
			t.Fatalf("ApplyStep(%T) returned error: %v", step, err)
		}
	}
}

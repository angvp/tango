package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// TestEnsureTrackingTableCreatesTimestampedTable checks the tracking table
// exists afterwards and that applied_at has the dialect's timestamp type:
// TIMESTAMPTZ on PostgreSQL, TIMESTAMP on SQLite.
func TestEnsureTrackingTableCreatesTimestampedTable(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		t.Fatalf("EnsureTrackingTable returned error: %v", err)
	}
	if !tableExists(t, sqlDB, dialect, "tango_migrations") {
		t.Fatal("tango_migrations table was not created")
	}

	query, want := "SELECT type FROM pragma_table_info('tango_migrations') WHERE name = 'applied_at'", "TIMESTAMP"
	if dialect == db.Postgres {
		query = "SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'tango_migrations' AND column_name = 'applied_at'"
		want = "timestamp with time zone"
	}
	var got string
	if err := sqlDB.QueryRowContext(ctx, query).Scan(&got); err != nil {
		t.Fatalf("look up applied_at type: %v", err)
	}
	if got != want {
		t.Fatalf("applied_at type = %q, want %q", got, want)
	}
}

func TestIsMissingTrackingTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "sqlite no such table", err: errors.New("no such table: tango_migrations"), want: true},
		{name: "sqlite no such table mixed case", err: errors.New("SQL logic error: no such table: tango_migrations"), want: true},
		{name: "postgres relation does not exist", err: errors.New(`pq: relation "tango_migrations" does not exist`), want: true},
		{name: "unrelated error", err: errors.New("connection refused"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMissingTrackingTable(tc.err); got != tc.want {
				t.Errorf("IsMissingTrackingTable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestTouchedAppsReturnsNilForEmptyApp(t *testing.T) {
	if apps := touchedApps(Migration{App: "", Name: "0001"}); apps != nil {
		t.Errorf("touchedApps with empty App = %v, want nil", apps)
	}
	if apps := touchedApps(Migration{App: "users", Name: "0001"}); len(apps) != 1 || apps[0] != "users" {
		t.Errorf("touchedApps with App=users = %v, want [users]", apps)
	}
}

func TestPlaceholderAtAcrossDialects(t *testing.T) {
	if got := placeholderAt(db.SQLite, 1); got != "?" {
		t.Errorf("placeholderAt(SQLite, 1) = %q, want %q", got, "?")
	}
	if got := placeholderAt(db.Postgres, 1); got != "$1" {
		t.Errorf("placeholderAt(Postgres, 1) = %q, want %q", got, "$1")
	}
	if got := placeholderAt(db.Postgres, 3); got != "$3" {
		t.Errorf("placeholderAt(Postgres, 3) = %q, want %q", got, "$3")
	}
}

func TestApplyPendingSurfacesEnsureTrackingTableError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err := ApplyPending(context.Background(), sqlDB, dialect, nil)
	if err == nil {
		t.Fatal("ApplyPending on a closed connection returned nil error, want an error")
	}
}

func TestApplyPendingSurfacesAppliedMigrationsQueryError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	// Pre-create a tango_migrations table missing the "name" column so
	// EnsureTrackingTable's CREATE TABLE IF NOT EXISTS is a no-op, and
	// AppliedMigrations' SELECT app, name then fails outright.
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE tango_migrations (app TEXT NOT NULL)`); err != nil {
		t.Fatalf("create broken tracking table: %v", err)
	}

	err := ApplyPending(ctx, sqlDB, dialect, nil)
	if err == nil {
		t.Fatal("ApplyPending with a malformed tango_migrations table returned nil error, want an error")
	}
}

func TestApplyPendingSurfacesApplyStepError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_bad", App: "users", Up: []Step{
			// AddColumn on a table that doesn't exist: ApplyStep fails.
			AddColumn{Table: "does_not_exist", Column: Column{Name: "x", Type: "text"}},
		}},
	}

	err := ApplyPending(ctx, sqlDB, dialect, migrations)
	if err == nil {
		t.Fatal("ApplyPending returned nil error for a failing Up step, want an error")
	}
}

// TestApplyPendingSurfacesTrackingInsertError forces the INSERT INTO
// tango_migrations row-recording statement to fail after the Up steps
// already ran, by blocking inserts with an ordinary SQL trigger — a
// realistic scenario (e.g. an audit trigger) rather than a contrived fault.
func TestApplyPendingSurfacesTrackingInsertError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		t.Fatalf("EnsureTrackingTable returned error: %v", err)
	}
	blockTrackingTable(t, sqlDB, dialect, "INSERT")

	migrations := []Migration{
		{Name: "0001_create_account", App: "users", Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}},
	}

	err := ApplyPending(ctx, sqlDB, dialect, migrations)
	if err == nil {
		t.Fatal("ApplyPending returned nil error when the tracking-row INSERT is blocked, want an error")
	}
	if !tableExists(t, sqlDB, dialect, "account") {
		t.Fatal("the Up step's CreateTable should still have run before the tracking insert failed")
	}
}

// blockTrackingTable installs a trigger that makes every operation
// ("INSERT" or "DELETE") on tango_migrations fail. Trigger syntax differs
// by dialect, so each gets its own raw SQL: SQLite raises from the trigger
// body, PostgreSQL from a PL/pgSQL function (created in the test's schema,
// so it goes when the schema does).
func blockTrackingTable(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, operation string) {
	t.Helper()
	name := "block_tracking_" + strings.ToLower(operation)
	statements := []string{fmt.Sprintf(
		"CREATE TRIGGER %s BEFORE %s ON tango_migrations BEGIN SELECT RAISE(ABORT, '%s blocked for test'); END",
		name, operation, operation,
	)}
	if dialect == db.Postgres {
		statements = []string{
			fmt.Sprintf(
				"CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION '%s blocked for test'; END $$",
				name, operation,
			),
			fmt.Sprintf(
				"CREATE TRIGGER %s BEFORE %s ON tango_migrations FOR EACH ROW EXECUTE FUNCTION %s()",
				name, operation, name,
			),
		}
	}
	for _, statement := range statements {
		if _, err := sqlDB.Exec(statement); err != nil {
			t.Fatalf("block %s on tango_migrations: %v", operation, err)
		}
	}
}

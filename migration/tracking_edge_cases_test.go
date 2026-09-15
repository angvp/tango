package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
)

// TestEnsureTrackingTableOnPostgresDialectGeneratesTimestamptzColumn is a
// direct assertion of EnsureTrackingTable's db.Postgres branch (TIMESTAMPTZ
// instead of TIMESTAMP) executed against the existing SQLite-backed test
// setup — SQLite accepts an arbitrary declared column type name, so this
// proves the branch is reached and produces working DDL without a live
// Postgres connection.
func TestEnsureTrackingTableOnPostgresDialectGeneratesTimestamptzColumn(t *testing.T) {
	sqlDB := openTrackingTestDB(t)
	ctx := context.Background()

	if err := EnsureTrackingTable(ctx, sqlDB, db.Postgres); err != nil {
		t.Fatalf("EnsureTrackingTable(db.Postgres) returned error: %v", err)
	}
	if !tableExists(t, sqlDB, "tango_migrations") {
		t.Fatal("tango_migrations table was not created")
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
	sqlDB := openTrackingTestDB(t)
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err := ApplyPending(context.Background(), sqlDB, db.SQLite, nil)
	if err == nil {
		t.Fatal("ApplyPending on a closed connection returned nil error, want an error")
	}
}

func TestApplyPendingSurfacesAppliedMigrationsQueryError(t *testing.T) {
	sqlDB := openTrackingTestDB(t)
	ctx := context.Background()
	// Pre-create a tango_migrations table missing the "name" column so
	// EnsureTrackingTable's CREATE TABLE IF NOT EXISTS is a no-op, and
	// AppliedMigrations' SELECT app, name then fails outright.
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE tango_migrations (app TEXT NOT NULL)`); err != nil {
		t.Fatalf("create broken tracking table: %v", err)
	}

	err := ApplyPending(ctx, sqlDB, db.SQLite, nil)
	if err == nil {
		t.Fatal("ApplyPending with a malformed tango_migrations table returned nil error, want an error")
	}
}

func TestApplyPendingSurfacesApplyStepError(t *testing.T) {
	sqlDB := openTrackingTestDB(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_bad", App: "users", Up: []Step{
			// AddColumn on a table that doesn't exist: ApplyStep fails.
			AddColumn{Table: "does_not_exist", Column: Column{Name: "x", Type: "text"}},
		}},
	}

	err := ApplyPending(ctx, sqlDB, db.SQLite, migrations)
	if err == nil {
		t.Fatal("ApplyPending returned nil error for a failing Up step, want an error")
	}
}

// TestApplyPendingSurfacesTrackingInsertError forces the INSERT INTO
// tango_migrations row-recording statement to fail after the Up steps
// already ran, by blocking inserts with an ordinary SQL trigger — a
// realistic scenario (e.g. an audit trigger) rather than a contrived fault.
func TestApplyPendingSurfacesTrackingInsertError(t *testing.T) {
	sqlDB := openTrackingTestDB(t)
	ctx := context.Background()

	if err := EnsureTrackingTable(ctx, sqlDB, db.SQLite); err != nil {
		t.Fatalf("EnsureTrackingTable returned error: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		CREATE TRIGGER block_tracking_insert BEFORE INSERT ON tango_migrations
		BEGIN SELECT RAISE(ABORT, 'insert blocked for test'); END;
	`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	migrations := []Migration{
		{Name: "0001_create_user", App: "users", Up: []Step{
			CreateTable{Table: "user", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}},
	}

	err := ApplyPending(ctx, sqlDB, db.SQLite, migrations)
	if err == nil {
		t.Fatal("ApplyPending returned nil error when the tracking-row INSERT is blocked, want an error")
	}
	if !tableExists(t, sqlDB, "user") {
		t.Fatal("the Up step's CreateTable should still have run before the tracking insert failed")
	}
}

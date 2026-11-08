package preflight

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

func conflictingMigrations() []migration.Migration {
	return []migration.Migration{
		{App: "blog", Name: "0005_add_email", Reversible: true,
			Up:   []migration.Step{migration.CreateTable{Table: "dc_email", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []migration.Step{migration.DropTable{Table: "dc_email"}}},
		{App: "blog", Name: "0005_add_phone", Reversible: true,
			Up:   []migration.Step{migration.CreateTable{Table: "dc_phone", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []migration.Step{migration.DropTable{Table: "dc_phone"}}},
	}
}

// The tracking table here has the wrong columns, so AppliedMigrations would
// fail with a database error. Validation must run first and report the
// conflict instead, which shows the -migrate path validates before any read.
func TestDispatchFlagsMigrateReportsConflictBeforeReadingTracking(t *testing.T) {
	sqlDB, _ := testdb.Open(t)
	if _, err := sqlDB.Exec("CREATE TABLE tango_migrations (x INTEGER)"); err != nil {
		t.Fatalf("creating malformed tracking table: %v", err)
	}

	withArgs(t, []string{"tango", "-migrate"}, func() {
		handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, testdb.Dialect(), conflictingMigrations())
		if !handled {
			t.Fatal("handled = false, want true")
		}
		if !errors.Is(err, migration.ErrConflictingMigrations) {
			t.Fatalf("err = %v, want ErrConflictingMigrations", err)
		}
	})
}

// tableAbsentErr returns nil only when the database answers a trivial query and
// its catalog holds no table named table. A closed or broken database fails the
// first step, so it is never read as an absent table.
func tableAbsentErr(sqlDB *sql.DB, dialect db.Dialect, table string) error {
	var one int
	if err := sqlDB.QueryRow("SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("connectivity check: %w", err)
	}
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name=?"
	switch dialect {
	case db.Postgres:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1"
	case db.MySQL:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = $1"
	}
	var name string
	err := sqlDB.QueryRow(query, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("catalog lookup for %q: %w", table, err)
	}
	return fmt.Errorf("table %q exists", table)
}

func TestTableAbsentErrRejectsClosedDatabase(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	sqlDB.Close()

	if err := tableAbsentErr(sqlDB, dialect, "tango_migrations"); err == nil {
		t.Fatal("tableAbsentErr on a closed database = nil, want an error")
	}
}

func TestTableAbsentErrDistinguishesAbsentFromPresent(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	if err := tableAbsentErr(sqlDB, dialect, "probe_missing"); err != nil {
		t.Fatalf("tableAbsentErr on a missing table: %v", err)
	}
	if _, err := sqlDB.Exec("CREATE TABLE probe_present (id INTEGER)"); err != nil {
		t.Fatalf("creating probe table: %v", err)
	}
	if err := tableAbsentErr(sqlDB, dialect, "probe_present"); err == nil {
		t.Fatal("tableAbsentErr on an existing table = nil, want an error")
	}
}

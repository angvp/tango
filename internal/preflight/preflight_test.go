package preflight

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

func validRegistry() []migration.Migration {
	return []migration.Migration{
		{App: "blog", Name: "0001_initial", Reversible: true,
			Up:   []migration.Step{migration.CreateTable{Table: "dc_post", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []migration.Step{migration.DropTable{Table: "dc_post"}}},
	}
}

func conflictingRegistry() []migration.Migration {
	return []migration.Migration{
		{App: "blog", Name: "0005_add_email", Reversible: true,
			Up:   []migration.Step{migration.CreateTable{Table: "dc_email", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []migration.Step{migration.DropTable{Table: "dc_email"}}},
		{App: "blog", Name: "0005_add_phone", Reversible: true,
			Up:   []migration.Step{migration.CreateTable{Table: "dc_phone", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
			Down: []migration.Step{migration.DropTable{Table: "dc_phone"}}},
	}
}

// withArgs sets os.Args for DispatchFlags, which parses them itself.
func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	original := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = original })
	fn()
}

// tableExists answers from the catalog of the dialect under test. A failed
// query is an error, never read as an absent table, so a closed or broken
// database cannot pass as an empty one.
func tableExists(sqlDB *sql.DB, dialect db.Dialect, table string) (bool, error) {
	var one int
	if err := sqlDB.QueryRow("SELECT 1").Scan(&one); err != nil {
		return false, fmt.Errorf("connectivity check: %w", err)
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
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("catalog lookup for %q: %w", table, err)
	}
	return true, nil
}

func assertAbsent(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, tables ...string) {
	t.Helper()
	for _, table := range tables {
		present, err := tableExists(sqlDB, dialect, table)
		if err != nil {
			t.Fatalf("table %q not confirmed absent: %v", table, err)
		}
		if present {
			t.Fatalf("table %q exists after a refused preflight", table)
		}
	}
}

func TestMigrateConflictLeavesFreshDatabaseUntouched(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	withArgs(t, []string{"tango", "-migrate"}, func() {
		handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, dialect, conflictingRegistry())
		if !handled {
			t.Fatal("handled = false, want true")
		}
		if !errors.Is(err, migration.ErrConflictingMigrations) {
			t.Fatalf("err = %v, want ErrConflictingMigrations", err)
		}
	})
	assertAbsent(t, sqlDB, dialect, "tango_migrations", "dc_email", "dc_phone")
}

func TestCheckConflictLeavesFreshDatabaseUntouched(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	withArgs(t, []string{"tango", "-check"}, func() {
		handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, dialect, conflictingRegistry())
		if !handled {
			t.Fatal("handled = false, want true")
		}
		if !errors.Is(err, migration.ErrConflictingMigrations) {
			t.Fatalf("err = %v, want ErrConflictingMigrations", err)
		}
	})
	assertAbsent(t, sqlDB, dialect, "tango_migrations", "dc_email", "dc_phone")
}

// The positive control: a valid -migrate on the same dialect must create the
// tracking table and the app table. Presence is read through the dialect's own
// catalog query, so this also shows the run reached the database under test.
func TestMigrateValidRegistryReachesDatabase(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	t.Logf("dialect under test: %s", dialectName(dialect))

	withArgs(t, []string{"tango", "-migrate"}, func() {
		handled, err := tango.DispatchFlags(tango.Config{}, sqlDB, dialect, validRegistry())
		if !handled || err != nil {
			t.Fatalf("handled = %v, err = %v; want handled and no error", handled, err)
		}
	})
	for _, table := range []string{"tango_migrations", "dc_post"} {
		present, err := tableExists(sqlDB, dialect, table)
		if err != nil {
			t.Fatalf("catalog lookup for %q: %v", table, err)
		}
		if !present {
			t.Fatalf("table %q missing after a valid -migrate", table)
		}
	}
}

func dialectName(dialect db.Dialect) string {
	switch dialect {
	case db.Postgres:
		return "postgres"
	case db.MySQL:
		return "mysql"
	case db.SQLite:
		return "sqlite"
	}
	return fmt.Sprintf("unknown(%d)", int(dialect))
}

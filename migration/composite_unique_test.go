package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func uniqueMigration(table string, up, down []Step) Migration {
	return Migration{App: "app", Name: "0002_" + table + "_unique", Up: up, Down: down, Reversible: true}
}

func baseMigration(table string) Migration {
	return Migration{App: "app", Name: "0001_" + table, Reversible: true,
		Up:   []Step{CreateTable{Table: table, Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "a", Type: "integer"}, {Name: "b", Type: "integer"}}}},
		Down: []Step{DropTable{Table: table}}}
}

func TestCompositeUniquePreflightRefusesDuplicatesBeforeAnyDDL(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	base := baseMigration("cu_dups")
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{base}); err != nil {
		t.Fatal(err)
	}
	for _, row := range [][2]any{{1, 1}, {1, 1}, {1, 1}, {2, 2}, {2, 2}, {3, 3}} {
		mustExec(t, sqlDB, "INSERT INTO cu_dups (a, b) VALUES ($1, $2)", row[0], row[1])
	}
	mustExec(t, sqlDB, "INSERT INTO cu_dups (a, b) VALUES (NULL, 9)")
	mustExec(t, sqlDB, "INSERT INTO cu_dups (a, b) VALUES (NULL, 9)")
	second := uniqueMigration("cu_dups",
		[]Step{
			AddColumn{Table: "cu_dups", Column: Column{Name: "extra", Type: "integer"}},
			CreateCompositeIndex{Table: "cu_dups", Columns: []string{"a", "b"}, Unique: true},
		},
		[]Step{DropCompositeIndex{Table: "cu_dups", Columns: []string{"a", "b"}, Unique: true}, DropColumn{Table: "cu_dups", Column: "extra"}})
	err := ApplyPending(ctx, sqlDB, dialect, []Migration{base, second})
	if err == nil {
		t.Fatal("a unique index over duplicate pairs was accepted")
	}
	for _, want := range []string{"cu_dups", "a, b", "2 duplicate groups", "id = "} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
	// The duplicated values (1, 2) are never quoted back: only the count and
	// one row identity appear.
	if strings.Contains(err.Error(), "9") {
		t.Fatalf("error %q mentions a value of the NULL rows, which are not duplicates", err)
	}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "cu_dups"); len(got) != 0 {
		t.Fatalf("the refused migration left an index: %#v", got)
	}
	if _, err := sqlDB.Exec("SELECT extra FROM cu_dups"); err == nil {
		t.Fatal("the refused migration ran its AddColumn: the check must come before any DDL")
	}
}

func TestCompositeUniquePreflightIgnoresNullMembersAndAcceptsDistinctPairs(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	base := baseMigration("cu_nulls")
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{base}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{"(NULL, 1)", "(NULL, 1)", "(1, NULL)", "(1, NULL)", "(NULL, NULL)", "(NULL, NULL)", "(1, 2)", "(2, 1)"} {
		mustExec(t, sqlDB, "INSERT INTO cu_nulls (a, b) VALUES "+row)
	}
	second := uniqueMigration("cu_nulls",
		[]Step{CreateCompositeIndex{Table: "cu_nulls", Columns: []string{"a", "b"}, Unique: true}},
		[]Step{DropCompositeIndex{Table: "cu_nulls", Columns: []string{"a", "b"}, Unique: true}})
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{base, second}); err != nil {
		t.Fatalf("rows with a NULL member are not duplicates: %v", err)
	}
	mustExec(t, sqlDB, "INSERT INTO cu_nulls (a, b) VALUES (NULL, 1)")
	_, err := sqlDB.Exec("INSERT INTO cu_nulls (a, b) VALUES (1, 2)")
	if err == nil || !db.IsUniqueConstraintViolation(err) {
		t.Fatalf("a duplicate pair after the migration: err = %v, want a unique violation", err)
	}
}

func TestCompositeUniquePreflightSkipsTablesAndColumnsTheMigrationCreates(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	m := Migration{App: "app", Name: "0001_new", Reversible: true,
		Up: []Step{
			CreateTable{Table: "cu_new", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "a", Type: "integer"}}},
			AddColumn{Table: "cu_new", Column: Column{Name: "b", Type: "integer"}},
			CreateCompositeIndex{Table: "cu_new", Columns: []string{"a", "b"}, Unique: true},
		},
		Down: []Step{DropTable{Table: "cu_new"}}}
	if err := ApplyPending(context.Background(), sqlDB, dialect, []Migration{m}); err != nil {
		t.Fatalf("a table the migration creates has nothing to check: %v", err)
	}
}

func TestCompositeUniquePreflightFollowsEarlierRenames(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	base := baseMigration("cu_ren")
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{base}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, sqlDB, "INSERT INTO cu_ren (a, b) VALUES (1, 1)")
	mustExec(t, sqlDB, "INSERT INTO cu_ren (a, b) VALUES (1, 1)")
	second := uniqueMigration("cu_ren",
		[]Step{
			RenameTable{From: "cu_ren", To: "cu_ren2"},
			RenameColumn{Table: "cu_ren2", From: "a", To: "x"},
			CreateCompositeIndex{Table: "cu_ren2", Columns: []string{"x", "b"}, Unique: true},
		},
		[]Step{DropCompositeIndex{Table: "cu_ren2", Columns: []string{"x", "b"}, Unique: true}, RenameColumn{Table: "cu_ren2", From: "x", To: "a"}, RenameTable{From: "cu_ren2", To: "cu_ren"}})
	err := ApplyPending(ctx, sqlDB, dialect, []Migration{base, second})
	if err == nil || !strings.Contains(err.Error(), "1 duplicate group") || !strings.Contains(err.Error(), "cu_ren") {
		t.Fatalf("error = %v, want a refusal counting one duplicate group in the table's database name", err)
	}
	if _, qerr := sqlDB.Exec("SELECT a FROM cu_ren"); qerr != nil {
		t.Fatalf("the refused migration renamed something before the check: %v", qerr)
	}
}

func TestCompositeUniquePreflightWithoutASinglePrimaryKeyOmitsTheIdentity(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	base := Migration{App: "app", Name: "0001_nopk", Reversible: true,
		Up:   []Step{CreateTable{Table: "cu_nopk", Columns: []Column{{Name: "a", Type: "integer"}, {Name: "b", Type: "integer"}}}},
		Down: []Step{DropTable{Table: "cu_nopk"}}}
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{base}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, sqlDB, "INSERT INTO cu_nopk (a, b) VALUES (1, 1)")
	mustExec(t, sqlDB, "INSERT INTO cu_nopk (a, b) VALUES (1, 1)")
	second := uniqueMigration("cu_nopk",
		[]Step{CreateCompositeIndex{Table: "cu_nopk", Columns: []string{"a", "b"}, Unique: true}},
		[]Step{DropCompositeIndex{Table: "cu_nopk", Columns: []string{"a", "b"}, Unique: true}})
	err := ApplyPending(ctx, sqlDB, dialect, []Migration{base, second})
	if err == nil || !strings.Contains(err.Error(), "1 duplicate group") {
		t.Fatalf("error = %v, want a refusal counting one group", err)
	}
	if strings.Contains(err.Error(), "for example") {
		t.Fatalf("error %q names a row though the table has no single primary key", err)
	}
}

func TestRollbackPreflightsAUniqueIndexItWouldRecreate(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	base := baseMigration("cu_back")
	// Applying the second migration drops the unique index and renames the
	// column; its Down recreates the index over the original names.
	second := uniqueMigration("cu_back",
		[]Step{CreateCompositeIndex{Table: "cu_back", Columns: []string{"a", "b"}, Unique: true}},
		[]Step{DropCompositeIndex{Table: "cu_back", Columns: []string{"a", "b"}, Unique: true}})
	third := Migration{App: "app", Name: "0003_cu_back_drop", Reversible: true,
		Up: []Step{
			DropCompositeIndex{Table: "cu_back", Columns: []string{"a", "b"}, Unique: true},
			RenameColumn{Table: "cu_back", From: "a", To: "x"},
		},
		Down: []Step{
			RenameColumn{Table: "cu_back", From: "x", To: "a"},
			CreateCompositeIndex{Table: "cu_back", Columns: []string{"a", "b"}, Unique: true},
		}}
	all := []Migration{base, second, third}
	if err := ApplyPending(ctx, sqlDB, dialect, all); err != nil {
		t.Fatal(err)
	}
	mustExec(t, sqlDB, "INSERT INTO cu_back (x, b) VALUES (1, 1)")
	mustExec(t, sqlDB, "INSERT INTO cu_back (x, b) VALUES (1, 1)")
	err := RollbackLast(ctx, sqlDB, dialect, all)
	if err == nil || !strings.Contains(err.Error(), "1 duplicate group") {
		t.Fatalf("RollbackLast error = %v, want a refusal counting one duplicate group", err)
	}
	if _, qerr := sqlDB.Exec("SELECT x FROM cu_back"); qerr != nil {
		t.Fatalf("the refused rollback renamed the column back before checking: %v", qerr)
	}
}

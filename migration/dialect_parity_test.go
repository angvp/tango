package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

// The behaviours below are promised on every dialect; they are the ones MySQL
// gets right only through its own DDL (table-level foreign keys, indexable
// text, case-sensitive collation), so the suite pins them for all three.

func TestForeignKeysAreEnforcedOnEveryDialect(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		CreateTable{Table: "book", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "author_id", Type: "integer", References: "author"},
		}},
	)
	mustFail(t, sqlDB, "a book needs an existing author", "INSERT INTO book (author_id) VALUES (999)")
	mustExec(t, sqlDB, "INSERT INTO author (id) VALUES (1)")
	mustExec(t, sqlDB, "INSERT INTO book (author_id) VALUES (1)")
	mustFail(t, sqlDB, "an author with books cannot be deleted", "DELETE FROM author WHERE id = 1")
}

func TestAddedForeignKeyColumnIsEnforcedAndDropsCleanly(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		CreateTable{Table: "book", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		AddColumn{Table: "book", Column: Column{Name: "author_id", Type: "integer", References: "author"}},
	)
	mustFail(t, sqlDB, "an added foreign key is enforced", "INSERT INTO book (author_id) VALUES (999)")
	applySteps(t, sqlDB, dialect, DropColumn{Table: "book", Column: "author_id"})
	mustExec(t, sqlDB, "INSERT INTO book (id) VALUES (1)")
}

func TestUniqueTextIsCaseSensitiveOnEveryDialect(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect, CreateTable{Table: "account", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "email", Type: "text", Unique: true},
	}})
	mustExec(t, sqlDB, "INSERT INTO account (email) VALUES ('Alice@example.com')")
	mustExec(t, sqlDB, "INSERT INTO account (email) VALUES ('alice@example.com')")
	mustFail(t, sqlDB, "the same text twice", "INSERT INTO account (email) VALUES ('alice@example.com')")
}

func TestUniquenessAddedLaterWorksOnAColumnCreatedAsPlainText(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "account", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text"},
		}},
		AlterColumnUnique{Table: "account", Column: "email", Unique: true},
		CreateIndex{Table: "account", Column: "email"},
	)
	mustExec(t, sqlDB, "INSERT INTO account (email) VALUES ('a')")
	mustFail(t, sqlDB, "duplicate after the unique index", "INSERT INTO account (email) VALUES ('a')")
	applySteps(t, sqlDB, dialect, DropIndex{Table: "account", Column: "email"}, AlterColumnUnique{Table: "account", Column: "email", Unique: false})
	mustExec(t, sqlDB, "INSERT INTO account (email) VALUES ('a')")
}

func TestHalfAppliedMigrationNamesWhatRanAndWritesNoTrackingRow(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	migrations := []Migration{{App: "shop", Name: "0001_two_steps", Up: []Step{
		CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		AddColumn{Table: "missing_table", Column: Column{Name: "color", Type: "text"}},
	}}}

	err := ApplyPending(ctx, sqlDB, dialect, migrations)

	if err == nil {
		t.Fatal("ApplyPending succeeded although its second step cannot run")
	}
	for _, want := range []string{"0001_two_steps", "step 2", "migration.AddColumn", "1 migration.CreateTable", "no tango_migrations row was written", "by hand"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if !tableExists(t, sqlDB, dialect, "widget") {
		t.Error("the first step's table should be left in place for the user to repair")
	}
	applied, err := AppliedMigrations(ctx, sqlDB)
	if err != nil || len(applied) != 0 {
		t.Fatalf("tracking rows = %v (%v), want none", applied, err)
	}
	if again := ApplyPending(ctx, sqlDB, dialect, migrations); again == nil {
		t.Fatal("a second run resumed or succeeded; a half-applied migration must be repaired by hand first")
	}
}

func TestFirstStepFailureIsReportedWithoutAnAppliedList(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	err := ApplyPending(context.Background(), sqlDB, dialect, []Migration{{App: "shop", Name: "0001_x", Up: []Step{
		AddColumn{Table: "missing_table", Column: Column{Name: "color", Type: "text"}},
	}}})
	if err == nil || strings.Contains(err.Error(), "left in place") {
		t.Fatalf("error = %v, want a plain failure when nothing had been applied", err)
	}
}

func TestMySQLTablesAreCaseSensitiveInnoDB(t *testing.T) {
	testdb.MySQLOnly(t, "reads MySQL's own table options")
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect, CreateTable{Table: "widget", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	var engine, collation string
	if err := sqlDB.QueryRow("SELECT engine, table_collation FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'widget'").Scan(&engine, &collation); err != nil {
		t.Fatal(err)
	}
	if engine != "InnoDB" || collation != "utf8mb4_bin" {
		t.Fatalf("engine %q collation %q, want InnoDB and utf8mb4_bin", engine, collation)
	}
}

func TestMySQLUnboundedUniqueTextBecomesVarchar255AndPlainTextIsLongtext(t *testing.T) {
	testdb.MySQLOnly(t, "MySQL cannot index unbounded text")
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect, CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "slug", Type: "text", Unique: true},
		{Name: "body", Type: "text"},
	}})
	if got := declaredType(t, sqlDB, dialect, "post", "slug"); got != "varchar(255)" {
		t.Errorf("slug = %q, want varchar(255)", got)
	}
	if got := declaredType(t, sqlDB, dialect, "post", "body"); got != "text" {
		t.Errorf("body = %q, want text (LONGTEXT)", got)
	}
}

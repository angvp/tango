package migration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

func countTrackingRows(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM tango_migrations").Scan(&count); err != nil {
		t.Fatalf("count tango_migrations: %v", err)
	}
	return count
}

func TestApplyPendingCreatesTablesAndTracksMigrations(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_create_account", App: "users", Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}},
	}

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	if !tableExists(t, sqlDB, dialect, "account") {
		t.Fatalf("table %q was not created", "account")
	}

	if countTrackingRows(t, sqlDB) != 1 {
		t.Fatalf("tango_migrations has %d rows, want 1", countTrackingRows(t, sqlDB))
	}

	var app, name string
	if err := sqlDB.QueryRow("SELECT app, name FROM tango_migrations").Scan(&app, &name); err != nil {
		t.Fatalf("select tango_migrations: %v", err)
	}
	if app != "users" || name != "0001_create_account" {
		t.Fatalf("got (%q, %q), want (%q, %q)", app, name, "users", "0001_create_account")
	}
}

func TestApplyPendingSkipsAlreadyAppliedMigrations(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{Name: "0001_create_account", App: "users", Up: []Step{
			CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}},
	}

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("first ApplyPending returned error: %v", err)
	}
	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("second ApplyPending returned error: %v", err)
	}

	if countTrackingRows(t, sqlDB) != 1 {
		t.Fatalf("tango_migrations has %d rows after re-running ApplyPending, want 1", countTrackingRows(t, sqlDB))
	}
}

func TestApplyPendingOnlyAppliesRemainingMigrations(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	first := Migration{Name: "0001_create_account", App: "users", Up: []Step{
		CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}
	second := Migration{Name: "0002_add_email", App: "users", Up: []Step{
		AddColumn{Table: "account", Column: Column{Name: "email", Type: "text"}},
	}}

	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{first}); err != nil {
		t.Fatalf("first ApplyPending returned error: %v", err)
	}
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{first, second}); err != nil {
		t.Fatalf("second ApplyPending returned error: %v", err)
	}

	names := columnNames(t, sqlDB, dialect, "account")
	if !contains(names, "email") {
		t.Fatalf("columns = %v, want %q", names, "email")
	}
	if countTrackingRows(t, sqlDB) != 2 {
		t.Fatalf("tango_migrations has %d rows, want 2", countTrackingRows(t, sqlDB))
	}
}

func TestApplyPendingRecordsOneRowPerAppTouched(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	usersMigration := Migration{Name: "0001_users", App: "users", Up: []Step{
		CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}
	postsMigration := Migration{Name: "0001_posts", App: "posts", Up: []Step{
		CreateTable{Table: "post", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}

	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{usersMigration, postsMigration}); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	if countTrackingRows(t, sqlDB) != 2 {
		t.Fatalf("tango_migrations has %d rows, want 2", countTrackingRows(t, sqlDB))
	}
}

func TestApplyPendingUsesAppAndNameAsIdentity(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	usersMigration := Migration{Name: "0001_auto", App: "users", Up: []Step{
		CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}
	postsMigration := Migration{Name: "0001_auto", App: "posts", Up: []Step{
		CreateTable{Table: "post", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}

	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{usersMigration}); err != nil {
		t.Fatalf("first ApplyPending returned error: %v", err)
	}
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{usersMigration, postsMigration}); err != nil {
		t.Fatalf("second ApplyPending returned error: %v", err)
	}

	if !tableExists(t, sqlDB, dialect, "post") {
		t.Fatalf("posts migration with same name was skipped")
	}
	if countTrackingRows(t, sqlDB) != 2 {
		t.Fatalf("tango_migrations has %d rows, want 2", countTrackingRows(t, sqlDB))
	}

	applied, err := AppliedMigrations(ctx, sqlDB)
	if err != nil {
		t.Fatalf("AppliedMigrations returned error: %v", err)
	}
	if !applied[MigrationKey{App: "users", Name: "0001_auto"}] {
		t.Fatalf("users/0001_auto missing from applied migrations")
	}
	if !applied[MigrationKey{App: "posts", Name: "0001_auto"}] {
		t.Fatalf("posts/0001_auto missing from applied migrations")
	}
}

type diffAuthor struct {
	ID int64 `tango:"pk"`
}

type diffArticle struct {
	ID       int64 `tango:"pk"`
	AuthorID int64 `tango:"fk=diffAuthor"`
}

// TestDiffedMigrationAppliesAndRollsBackWhenAModelReferencesALaterTable
// covers a model whose table sorts before the table it references
// (diff_article -> diff_author): PostgreSQL refuses a REFERENCES clause
// naming a table that does not exist yet, and refuses to drop a table
// another table still references, so Diff's Up and Down steps must follow
// the foreign keys rather than table-name order.
func TestDiffedMigrationAppliesAndRollsBackWhenAModelReferencesALaterTable(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	registry := model.NewRegistry()
	registry.SetCurrentApp("blog")
	for _, value := range []any{diffArticle{}, diffAuthor{}} {
		if err := registry.Register(value); err != nil {
			t.Fatalf("Register(%T) returned error: %v", value, err)
		}
	}
	migrations := mustDiff(t, registry.All(), SchemaState{Tables: map[string]TableState{}})
	if len(migrations) != 1 {
		t.Fatalf("Diff returned %d migrations, want 1", len(migrations))
	}
	migrations[0].Name = "0001_initial"

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO diff_article (author_id) VALUES (999)"); err == nil {
		t.Fatal("insert with dangling author_id succeeded, want a foreign-key violation")
	}

	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("RollbackLast returned error: %v", err)
	}
	for _, table := range []string{"diff_article", "diff_author"} {
		if tableExists(t, sqlDB, dialect, table) {
			t.Fatalf("table %q still exists after rollback", table)
		}
	}
}

// TestDiffedMigrationDropsARemovedReferencedTableAfterItsReferrers covers
// removing two models at once where the referencing table sorts after the
// one it references (ref_post -> ref_author): PostgreSQL refuses to drop
// ref_author while ref_post still references it.
func TestDiffedMigrationDropsARemovedReferencedTableAfterItsReferrers(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	initial := Migration{Name: "0001_initial", App: "blog", Reversible: true, Up: []Step{
		CreateTable{Table: "ref_author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		CreateTable{Table: "ref_post", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "author_id", Type: "integer", References: "ref_author"},
		}},
	}}
	state, err := Replay([]Migration{initial})
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}
	removal := mustDiff(t, nil, state)
	if len(removal) != 1 {
		t.Fatalf("Diff returned %d migrations, want 1", len(removal))
	}
	removal[0].Name = "0002_remove_blog"

	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{initial, removal[0]}); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}
	for _, table := range []string{"ref_author", "ref_post"} {
		if tableExists(t, sqlDB, dialect, table) {
			t.Fatalf("table %q still exists after its model was removed", table)
		}
	}
}

package migration

import (
	"context"
	"database/sql"
	"testing"

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

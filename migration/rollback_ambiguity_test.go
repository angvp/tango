package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// sharedAppliedAt is written over every seeded tracking row, so the rows tie
// by construction and never by the database's timestamp resolution.
var sharedAppliedAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// seedTrackingTie records one tango_migrations row per key, then sets all of
// them to sharedAppliedAt in an explicit UPDATE.
func seedTrackingTie(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, keys ...MigrationKey) {
	t.Helper()
	ctx := context.Background()
	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		t.Fatalf("EnsureTrackingTable: %v", err)
	}
	for _, key := range keys {
		if _, err := sqlDB.ExecContext(ctx, trackingInsertSQL(dialect), key.App, key.Name, time.Unix(0, 0).UTC()); err != nil {
			t.Fatalf("insert tracking row %s/%s: %v", key.App, key.Name, err)
		}
	}
	if _, err := sqlDB.ExecContext(ctx, "UPDATE tango_migrations SET applied_at = "+placeholderAt(dialect, 1), sharedAppliedAt); err != nil {
		t.Fatalf("setting shared applied_at: %v", err)
	}
}

// trackingSnapshot returns every tango_migrations row, so a test can prove a
// refused rollback left the rows, including applied_at, exactly as they were.
func trackingSnapshot(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	rows, err := sqlDB.Query("SELECT app, name, applied_at FROM tango_migrations ORDER BY app, name")
	if err != nil {
		t.Fatalf("snapshot tango_migrations: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var app, name string
		var appliedAt any
		if err := rows.Scan(&app, &name, &appliedAt); err != nil {
			t.Fatalf("scan tango_migrations: %v", err)
		}
		fmt.Fprintf(&b, "%s/%s@%v\n", app, name, appliedAt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tango_migrations: %v", err)
	}
	return b.String()
}

// TestRollbackLastRefusesTieThatCannotBeOrdered: the two tied migrations
// reference each other's table, so applyOrder reports a cycle. The tables are
// created without their foreign keys so the fixture can exist; RollbackLast
// never reads the Up steps.
func TestRollbackLastRefusesTieThatCannotBeOrdered(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	registry := []Migration{
		{App: "cyclea", Name: "0001_initial", Reversible: true,
			Up: []Step{CreateTable{Table: "cyc_a", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "b_id", Type: "integer", References: "cyc_b"},
			}}},
			Down: []Step{DropTable{Table: "cyc_a"}}},
		{App: "cycleb", Name: "0001_initial", Reversible: true,
			Up: []Step{CreateTable{Table: "cyc_b", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "a_id", Type: "integer", References: "cyc_a"},
			}}},
			Down: []Step{DropTable{Table: "cyc_b"}}},
	}
	for _, table := range []string{"cyc_a", "cyc_b"} {
		if err := ApplyStep(ctx, sqlDB, dialect, CreateTable{Table: table, Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}}); err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
	}
	seedTrackingTie(t, sqlDB, dialect,
		MigrationKey{App: "cyclea", Name: "0001_initial"},
		MigrationKey{App: "cycleb", Name: "0001_initial"},
	)
	before := trackingSnapshot(t, sqlDB)

	err := RollbackLast(ctx, sqlDB, dialect, registry)
	if !errors.Is(err, ErrUnorderableTie) {
		t.Fatalf("RollbackLast error = %v, want ErrUnorderableTie", err)
	}
	for _, want := range []string{"cyclea/0001_initial", "cycleb/0001_initial", "2026-10-07"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	for _, table := range []string{"cyc_a", "cyc_b"} {
		if !tableExists(t, sqlDB, dialect, table) {
			t.Fatalf("table %q was dropped by a refused rollback", table)
		}
	}
	if after := trackingSnapshot(t, sqlDB); after != before {
		t.Fatalf("tango_migrations changed by a refused rollback:\nbefore:\n%safter:\n%s", before, after)
	}
}

// TestRollbackLastRefusesTieWhenLaterTiedKeyIsUnregistered: zapp/0001_a is
// registered and is the first tied row in DESC order; aapp/0001_a is tied and
// not registered. The refusal must name the unregistered key, so a rollback
// cannot proceed on the registered row alone.
func TestRollbackLastRefusesTieWhenLaterTiedKeyIsUnregistered(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	zapp := Migration{App: "zapp", Name: "0001_a", Reversible: true,
		Up:   []Step{CreateTable{Table: "tie_z", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
		Down: []Step{DropTable{Table: "tie_z"}},
	}
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{zapp}); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	seedTrackingTie(t, sqlDB, dialect, MigrationKey{App: "aapp", Name: "0001_a"})
	before := trackingSnapshot(t, sqlDB)

	err := RollbackLast(ctx, sqlDB, dialect, []Migration{zapp})
	if !errors.Is(err, ErrUnorderableTie) {
		t.Fatalf("RollbackLast error = %v, want ErrUnorderableTie", err)
	}
	if !strings.Contains(err.Error(), "aapp/0001_a") {
		t.Fatalf("error %q does not name the unregistered tied key aapp/0001_a", err)
	}
	if !tableExists(t, sqlDB, dialect, "tie_z") {
		t.Fatal("tie_z was dropped by a refused rollback")
	}
	if after := trackingSnapshot(t, sqlDB); after != before {
		t.Fatalf("tango_migrations changed by a refused rollback:\nbefore:\n%safter:\n%s", before, after)
	}
}

// TestRollbackLastRefusesDuplicateRegistryEntryForTarget: the applied
// users/0001_create_account appears twice in the registry with different Down
// lists. Neither copy is authoritative, so the rollback must refuse and run no
// Down step.
func TestRollbackLastRefusesDuplicateRegistryEntryForTarget(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	account := Migration{Name: "0001_create_account", App: "users", Reversible: true,
		Up:   []Step{CreateTable{Table: "account", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
		Down: []Step{DropTable{Table: "account"}},
	}
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{account}); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	before := trackingSnapshot(t, sqlDB)

	shadow := account
	shadow.Down = []Step{DropTable{Table: "never_created"}}
	err := RollbackLast(ctx, sqlDB, dialect, []Migration{account, shadow})
	if !errors.Is(err, ErrAmbiguousRollbackTarget) {
		t.Fatalf("RollbackLast error = %v, want ErrAmbiguousRollbackTarget", err)
	}
	if !strings.Contains(err.Error(), `"users"/"0001_create_account"`) {
		t.Fatalf("error %q does not name users/0001_create_account", err)
	}
	if !tableExists(t, sqlDB, dialect, "account") {
		t.Fatal("account was dropped by a refused rollback")
	}
	if after := trackingSnapshot(t, sqlDB); after != before {
		t.Fatalf("tango_migrations changed by a refused rollback:\nbefore:\n%safter:\n%s", before, after)
	}
}

// TestRollbackLastAllowsSameNumberWithDifferentNames: identity is (App, Name),
// so two names sharing the number 0001 in one app do not block rollback of
// the one that is applied.
func TestRollbackLastAllowsSameNumberWithDifferentNames(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	first := Migration{Name: "0001_a", App: "users", Reversible: true,
		Up:   []Step{CreateTable{Table: "tbl_a", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
		Down: []Step{DropTable{Table: "tbl_a"}},
	}
	second := Migration{Name: "0001_b", App: "users", Reversible: true,
		Up:   []Step{CreateTable{Table: "tbl_b", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}},
		Down: []Step{DropTable{Table: "tbl_b"}},
	}
	if err := ApplyPending(ctx, sqlDB, dialect, []Migration{first}); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	if err := RollbackLast(ctx, sqlDB, dialect, []Migration{first, second}); err != nil {
		t.Fatalf("RollbackLast returned error: %v", err)
	}
	if tableExists(t, sqlDB, dialect, "tbl_a") {
		t.Fatal("tbl_a still exists after rolling back users/0001_a")
	}
	if countTrackingRows(t, sqlDB) != 0 {
		t.Fatalf("tango_migrations has %d rows, want 0", countTrackingRows(t, sqlDB))
	}
}

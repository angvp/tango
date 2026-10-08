package migration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/testdb"
)

// TestApplyPendingCreatesAReferencedAppsTableFirstAndRollsBackInReverse
// covers a foreign key from one app's model to another app's model where
// the referencing app's migration sorts first (by app name, or by migration
// name): PostgreSQL refuses a REFERENCES clause naming a table that does
// not exist yet, and refuses to drop a table another table still
// references, so ApplyPending and RollbackLast must follow the cross-app
// foreign key rather than name order.
func TestApplyPendingCreatesAReferencedAppsTableFirstAndRollsBackInReverse(t *testing.T) {
	tests := []struct {
		name            string
		sameInstant     bool
		referencingApp  string
		referencingName string
		referencedApp   string
		referencedName  string
	}{
		{
			name:            "referencing app name sorts first",
			referencingApp:  "aposts",
			referencingName: "0001_initial",
			referencedApp:   "zauthors",
			referencedName:  "0001_initial",
		},
		{
			name:            "referencing migration name sorts first",
			referencingApp:  "zposts",
			referencingName: "0001_auto_20260101000000",
			referencedApp:   "aauthors",
			referencedName:  "0001_auto_20260202000000",
		},
		{
			name:            "both applied within one clock tick",
			sameInstant:     true,
			referencingApp:  "aposts",
			referencingName: "0001_initial",
			referencedApp:   "zauthors",
			referencedName:  "0001_initial",
		},
		{
			name:            "both applied within one clock tick, referencing migration name sorts first",
			sameInstant:     true,
			referencingApp:  "zposts",
			referencingName: "0001_auto_20260101000000",
			referencedApp:   "aauthors",
			referencedName:  "0001_auto_20260202000000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, dialect := testdb.Open(t)
			ctx := context.Background()

			authors := Migration{App: tt.referencedApp, Name: tt.referencedName, Reversible: true,
				Up: []Step{CreateTable{Table: "xapp_author", Columns: []Column{
					{Name: "id", Type: "integer", PrimaryKey: true},
				}}},
				Down: []Step{DropTable{Table: "xapp_author"}},
			}
			posts := Migration{App: tt.referencingApp, Name: tt.referencingName, Reversible: true,
				Up: []Step{CreateTable{Table: "xapp_post", Columns: []Column{
					{Name: "id", Type: "integer", PrimaryKey: true},
					{Name: "author_id", Type: "integer", References: "xapp_author"},
				}}},
				Down: []Step{DropTable{Table: "xapp_post"}},
			}
			// InstalledApps order: the referenced app first.
			migrations := []Migration{authors, posts}

			if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
				t.Fatalf("ApplyPending returned error: %v", err)
			}
			if _, err := sqlDB.ExecContext(ctx, "INSERT INTO xapp_post (author_id) VALUES (999)"); err == nil {
				t.Fatal("insert with dangling author_id succeeded, want a foreign-key violation")
			}
			if tt.sameInstant {
				// A fast run can record both rows with an identical
				// applied_at; RollbackLast must still undo them in reverse.
				instant := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
				if _, err := sqlDB.ExecContext(ctx, "UPDATE tango_migrations SET applied_at = "+placeholderAt(dialect, 1), instant); err != nil {
					t.Fatalf("setting a shared applied_at: %v", err)
				}
			}

			for i := 0; i < 2; i++ {
				if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
					t.Fatalf("RollbackLast #%d returned error: %v", i+1, err)
				}
			}
			for _, table := range []string{"xapp_post", "xapp_author"} {
				if tableExists(t, sqlDB, dialect, table) {
					t.Fatalf("table %q still exists after rollback", table)
				}
			}
			if err := RollbackLast(ctx, sqlDB, dialect, migrations); !errors.Is(err, ErrNoAppliedMigrations) {
				t.Fatalf("third RollbackLast error = %v, want ErrNoAppliedMigrations", err)
			}
		})
	}
}

// TestApplyPendingRejectsForeignKeysBetweenAppsThatFormACycle covers two
// apps whose initial migrations each reference the other's table: no order
// can create both on PostgreSQL, so ApplyPending fails up front, naming both
// apps, and applies nothing.
func TestApplyPendingRejectsForeignKeysBetweenAppsThatFormACycle(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	migrations := []Migration{
		{App: "cyclea", Name: "0001_initial", Reversible: true, Up: []Step{CreateTable{Table: "cyc_a", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "b_id", Type: "integer", References: "cyc_b"},
		}}}},
		{App: "cycleb", Name: "0001_initial", Reversible: true, Up: []Step{CreateTable{Table: "cyc_b", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "a_id", Type: "integer", References: "cyc_a"},
		}}}},
	}

	err := ApplyPending(ctx, sqlDB, dialect, migrations)
	if err == nil {
		t.Fatal("ApplyPending returned nil, want a foreign-key cycle error")
	}
	for _, want := range []string{"cycle", "cyclea/0001_initial", "cycleb/0001_initial"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ApplyPending error = %q, want it to mention %q", err, want)
		}
	}
	for _, table := range []string{"cyc_a", "cyc_b"} {
		if tableExists(t, sqlDB, dialect, table) {
			t.Fatalf("table %q exists after a rejected ApplyPending", table)
		}
	}
}

// TestApplyPendingOrdersOneMigrationsTablesByForeignKey covers a migration
// file written by a release before v0.1.0, whose generator ordered a
// migration's CreateTable steps (and their DropTable Down steps) by table
// name: here comment, which references post, is created first and dropped
// last. PostgreSQL refuses both, so the runner orders each run of
// CreateTable steps referenced-first and each run of DropTable steps
// referencing-first; the Generated-file contract needs those files to keep
// applying and rolling back.
func TestApplyPendingOrdersOneMigrationsTablesByForeignKey(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect := testdb.Open(t)
	migrations := []Migration{{
		App: "blog", Name: "0001_initial", Reversible: true,
		Up: []Step{
			CreateTable{Table: "author", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
			CreateTable{Table: "comment", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "post_id", Type: "integer", References: "post"},
			}},
			CreateTable{Table: "post", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "author_id", Type: "integer", References: "author"},
			}},
		},
		Down: []Step{DropTable{Table: "author"}, DropTable{Table: "comment"}, DropTable{Table: "post"}},
	}}

	if err := ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO comment (id, post_id) VALUES (1, 99)`); err == nil {
		t.Fatal("comment.post_id accepted a post that does not exist; want its foreign key enforced")
	}
	if err := RollbackLast(ctx, sqlDB, dialect, migrations); err != nil {
		t.Fatalf("RollbackLast: %v", err)
	}
	for _, table := range []string{"author", "comment", "post"} {
		if _, err := sqlDB.ExecContext(ctx, `SELECT 1 FROM `+table); err == nil {
			t.Fatalf("table %s still exists after rolling back", table)
		}
	}
}

// These tests enforce the Generated-file contract: every migration file a
// released `tango makemigrations` wrote keeps compiling, applying and
// rolling back on this version. The header read-back half lives with the
// CLI, in internal/cli.
package migrationcompat_test

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/internal/migrationcompat"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

// TestGeneratedMigrationsApplyAndRollBack applies each version's migrations
// one at a time to a fresh database and, after each reversible one, rolls it
// back and applies it again, so every Up and every reversible Down runs
// against the schema the generator wrote it for.
func TestGeneratedMigrationsApplyAndRollBack(t *testing.T) {
	ctx := context.Background()
	for _, gen := range migrationcompat.Generators {
		all := gen.Migrations
		t.Run(gen.Dir, func(t *testing.T) {
			sqlDB, dialect := testdb.Open(t)
			for i, m := range all {
				applied := all[:i+1]
				if err := migration.ApplyPending(ctx, sqlDB, dialect, applied); err != nil {
					t.Fatalf("apply %s: %v", m.Name, err)
				}
				if !m.Reversible {
					if err := migration.RollbackLast(ctx, sqlDB, dialect, applied); !errors.Is(err, migration.ErrIrreversibleMigration) {
						t.Fatalf("roll back irreversible %s: error = %v, want ErrIrreversibleMigration", m.Name, err)
					}
					continue
				}
				if err := migration.RollbackLast(ctx, sqlDB, dialect, applied); err != nil {
					t.Fatalf("roll back %s: %v", m.Name, err)
				}
				if err := migration.ApplyPending(ctx, sqlDB, dialect, applied); err != nil {
					t.Fatalf("reapply %s after rolling it back: %v", m.Name, err)
				}
			}
		})
	}
}

// TestGeneratedMigrationsRollBackToEmpty rolls back each version's reversible
// prefix, newest first, after applying only that prefix: the first
// migration's Down must drop the tables it created, whatever their foreign
// keys.
func TestGeneratedMigrationsRollBackToEmpty(t *testing.T) {
	ctx := context.Background()
	for _, gen := range migrationcompat.Generators {
		all := gen.Migrations
		t.Run(gen.Dir, func(t *testing.T) {
			prefix := all
			for i, m := range all {
				if !m.Reversible {
					prefix = all[:i]
					break
				}
			}
			sqlDB, dialect := testdb.Open(t)
			if err := migration.ApplyPending(ctx, sqlDB, dialect, prefix); err != nil {
				t.Fatalf("apply: %v", err)
			}
			for range prefix {
				if err := migration.RollbackLast(ctx, sqlDB, dialect, prefix); err != nil {
					t.Fatalf("roll back: %v", err)
				}
			}
			if err := migration.RollbackLast(ctx, sqlDB, dialect, prefix); !errors.Is(err, migration.ErrNoAppliedMigrations) {
				t.Fatalf("roll back past the first migration: error = %v, want ErrNoAppliedMigrations", err)
			}
		})
	}
}

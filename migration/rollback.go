package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/angvp/tango/db"
)

// ErrNoAppliedMigrations is returned by RollbackLast when tango_migrations
// has nothing recorded to roll back.
var ErrNoAppliedMigrations = errors.New("tango migration: no applied migrations to roll back")

// ErrIrreversibleMigration is returned by RollbackLast when the most
// recently applied migration is marked irreversible.
var ErrIrreversibleMigration = errors.New("tango migration: migration is irreversible")

// RollbackLast rolls back the most recently applied migration (by
// applied_at) by running its Down steps, then removes its tango_migrations
// row(s). It fails, changing nothing, if that migration is marked
// irreversible in migrations, or if migrations doesn't contain it.
func RollbackLast(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, migrations []Migration) error {
	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		return err
	}

	lastKey, err := lastAppliedKey(ctx, sqlDB)
	if err != nil {
		return err
	}
	if lastKey == (MigrationKey{}) {
		return ErrNoAppliedMigrations
	}

	var target *Migration
	for i := range migrations {
		if migrations[i].App == lastKey.App && migrations[i].Name == lastKey.Name {
			target = &migrations[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("tango migration: applied migration %q/%q not found among provided migrations", lastKey.App, lastKey.Name)
	}
	if !target.Reversible {
		return fmt.Errorf("%w: %q", ErrIrreversibleMigration, target.Name)
	}

	for _, step := range target.Down {
		if err := ApplyStep(ctx, sqlDB, dialect, step); err != nil {
			return fmt.Errorf("migration %q: rolling back: %w", target.Name, err)
		}
	}

	if _, err := sqlDB.ExecContext(ctx,
		"DELETE FROM tango_migrations WHERE app = "+placeholderAt(dialect, 1)+" AND name = "+placeholderAt(dialect, 2),
		target.App, target.Name,
	); err != nil {
		return fmt.Errorf("migration %q: removing tango_migrations rows: %w", target.Name, err)
	}

	return nil
}

func lastAppliedKey(ctx context.Context, sqlDB *sql.DB) (MigrationKey, error) {
	var key MigrationKey
	err := sqlDB.QueryRowContext(ctx, "SELECT app, name FROM tango_migrations ORDER BY applied_at DESC, name DESC, app DESC LIMIT 1").Scan(&key.App, &key.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return MigrationKey{}, nil
	}
	if err != nil {
		return MigrationKey{}, err
	}
	return key, nil
}

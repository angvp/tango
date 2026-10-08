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
// applied_at; among migrations applied at the same instant, the last one in
// ApplyPending's order) by running its Down steps, then removes its tango_migrations
// row(s). It fails, changing nothing, if that migration is marked
// irreversible in migrations, or if migrations doesn't contain it.
func RollbackLast(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, migrations []Migration) error {
	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		return err
	}

	lastKey, err := lastAppliedKey(ctx, sqlDB, migrations)
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

	for _, step := range stepOrder(*target, target.Down) {
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

// lastAppliedKey returns the migration RollbackLast undoes: the one with the
// latest applied_at. Migrations recorded with the same applied_at (one fast
// ApplyPending run) are undone in the reverse of the order ApplyPending runs
// them, so a table another app's table references is dropped last.
func lastAppliedKey(ctx context.Context, sqlDB *sql.DB, migrations []Migration) (MigrationKey, error) {
	rows, err := sqlDB.QueryContext(ctx,
		"SELECT app, name FROM tango_migrations WHERE applied_at = (SELECT MAX(applied_at) FROM tango_migrations) ORDER BY name DESC, app DESC")
	if err != nil {
		return MigrationKey{}, err
	}
	defer rows.Close()

	var tied []MigrationKey
	for rows.Next() {
		var key MigrationKey
		if err := rows.Scan(&key.App, &key.Name); err != nil {
			return MigrationKey{}, err
		}
		tied = append(tied, key)
	}
	if err := rows.Err(); err != nil {
		return MigrationKey{}, err
	}
	if len(tied) == 0 {
		return MigrationKey{}, nil
	}
	return lastOfTied(tied, migrations), nil
}

// lastOfTied picks which of several migrations sharing the latest
// applied_at to undo first: the last one in apply order. It falls back to
// tied[0] (latest by name, then app) when there is only one, when one of
// them isn't among migrations (RollbackLast then reports it), or when they
// can't be ordered.
func lastOfTied(tied []MigrationKey, migrations []Migration) MigrationKey {
	if len(tied) == 1 {
		return tied[0]
	}
	byKey := make(map[MigrationKey]Migration, len(migrations))
	for _, m := range migrations {
		byKey[MigrationKey{App: m.App, Name: m.Name}] = m
	}
	candidates := make([]Migration, 0, len(tied))
	for _, key := range tied {
		m, ok := byKey[key]
		if !ok {
			return tied[0]
		}
		candidates = append(candidates, m)
	}
	ordered, err := applyOrder(candidates)
	if err != nil {
		return tied[0]
	}
	last := ordered[len(ordered)-1]
	return MigrationKey{App: last.App, Name: last.Name}
}

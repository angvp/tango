package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
)

// ErrNoAppliedMigrations is returned by RollbackLast when tango_migrations
// has nothing recorded to roll back.
var ErrNoAppliedMigrations = errors.New("tango migration: no applied migrations to roll back")

// ErrIrreversibleMigration is returned by RollbackLast when the most
// recently applied migration is marked irreversible.
var ErrIrreversibleMigration = errors.New("tango migration: migration is irreversible")

// ErrUnorderableTie is returned by RollbackLast when several tracked rows share
// the latest applied_at and cannot be ordered, so no single one is the last
// applied. It is a rollback-only sentinel: ErrConflictingMigrations is the
// registry validator's error and does not describe a recovery-path refusal.
var ErrUnorderableTie = errors.New("tango migration: cannot choose the last applied migration")

// ErrAmbiguousRollbackTarget is returned by RollbackLast when the (App, Name)
// it would roll back appears more than once in the registry, so the registry
// does not say which Down steps belong to it.
var ErrAmbiguousRollbackTarget = errors.New("tango migration: rollback target is ambiguous in the registry")

// RollbackLast rolls back the most recently applied migration (by
// applied_at; among migrations applied at the same instant, the last one in
// ApplyPending's order) by running its Down steps, then removes its tango_migrations
// row(s). It fails, changing nothing, if that migration is marked
// irreversible in migrations, or if migrations doesn't contain it. Refusals
// happen before any Down runs: when the rows tied at the latest applied_at
// cannot be ordered, and when the target appears more than once in migrations.
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

	target, err := uniqueCopy(migrations, lastKey)
	if errors.Is(err, errNotInRegistry) {
		return fmt.Errorf("tango migration: applied migration %q/%q not found among provided migrations", lastKey.App, lastKey.Name)
	}
	if err != nil {
		return err
	}
	if !target.Reversible {
		return fmt.Errorf("%w: %q%s", ErrIrreversibleMigration, target.Name, wideningHint(target))
	}

	for _, step := range stepOrder(target, target.Down) {
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
		"SELECT app, name, applied_at FROM tango_migrations WHERE applied_at = (SELECT MAX(applied_at) FROM tango_migrations) ORDER BY name DESC, app DESC")
	if err != nil {
		return MigrationKey{}, err
	}
	defer rows.Close()

	var tied []trackedRow
	for rows.Next() {
		var row trackedRow
		if err := rows.Scan(&row.key.App, &row.key.Name, &row.appliedAt); err != nil {
			return MigrationKey{}, err
		}
		tied = append(tied, row)
	}
	if err := rows.Err(); err != nil {
		return MigrationKey{}, err
	}
	if len(tied) == 0 {
		return MigrationKey{}, nil
	}
	return lastOfTied(tied, migrations)
}

// trackedRow is one tango_migrations row that shares the latest applied_at.
// appliedAt is kept as the driver returned it so it can be shown back to the
// operator unchanged.
type trackedRow struct {
	key       MigrationKey
	appliedAt any
}

// lastOfTied picks which of several migrations sharing the latest applied_at
// to undo first: the last one in apply order. A single row needs no ordering.
// Several rows must all be in the registry, each exactly once, and must order
// under applyOrder; otherwise it returns an error and the caller runs no Down.
func lastOfTied(tied []trackedRow, migrations []Migration) (MigrationKey, error) {
	if len(tied) == 1 {
		return tied[0].key, nil
	}
	candidates, err := tiedCandidates(tied, migrations)
	if err != nil {
		return MigrationKey{}, err
	}
	ordered, err := applyOrder(candidates)
	if err != nil {
		return MigrationKey{}, unorderableTie(tied, fmt.Sprintf("cannot be ordered: %v", err))
	}
	last := ordered[len(ordered)-1]
	return MigrationKey{App: last.App, Name: last.Name}, nil
}

// tiedCandidates resolves each tied row to its single registry entry. A row
// absent from the registry is an unorderable tie; a duplicated entry is passed
// through as ErrAmbiguousRollbackTarget.
func tiedCandidates(tied []trackedRow, migrations []Migration) ([]Migration, error) {
	candidates := make([]Migration, 0, len(tied))
	for _, row := range tied {
		candidate, err := uniqueCopy(migrations, row.key)
		if errors.Is(err, errNotInRegistry) {
			return nil, unorderableTie(tied, fmt.Sprintf("%s/%s is not in the registry", row.key.App, row.key.Name))
		}
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// unorderableTie wraps ErrUnorderableTie with the tied rows and the reason
// they could not be ordered.
func unorderableTie(tied []trackedRow, reason string) error {
	keys := make([]string, len(tied))
	for i, row := range tied {
		keys[i] = row.key.App + "/" + row.key.Name
	}
	return fmt.Errorf("%w: rows %s share applied_at %s, and %s",
		ErrUnorderableTie, strings.Join(keys, ", "), formatAppliedAt(tied[0].appliedAt), reason)
}

// errNotInRegistry is returned by uniqueCopy when no registry entry matches.
// Callers wrap it in their own message, because the two rollback paths word
// the refusal differently.
var errNotInRegistry = errors.New("tango migration: not in registry")

// uniqueCopy returns the single registry entry for key. Several entries would
// leave the Down steps unattributable, so that case is refused.
func uniqueCopy(migrations []Migration, key MigrationKey) (Migration, error) {
	copies := registryCopies(migrations, key)
	switch len(copies) {
	case 0:
		return Migration{}, errNotInRegistry
	case 1:
		return copies[0], nil
	default:
		return Migration{}, ambiguousTargetError(key, len(copies))
	}
}

// registryCopies returns every registry entry whose (App, Name) is key.
func registryCopies(migrations []Migration, key MigrationKey) []Migration {
	var copies []Migration
	for _, m := range migrations {
		if m.App == key.App && m.Name == key.Name {
			copies = append(copies, m)
		}
	}
	return copies
}

// formatAppliedAt renders a driver value of applied_at for an error message.
// Some drivers return text columns as []byte, which would otherwise print as
// a list of byte values.
func formatAppliedAt(value any) string {
	if b, ok := value.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(value)
}

func ambiguousTargetError(key MigrationKey, count int) error {
	return fmt.Errorf("%w: %q/%q appears %d times in the registry, so its Down steps are not defined",
		ErrAmbiguousRollbackTarget, key.App, key.Name, count)
}

// wideningHint explains why a migration that widens a bounded string cannot
// be rolled back, and what to do instead.
func wideningHint(m Migration) string {
	for _, step := range m.Up {
		if s, ok := step.(AlterColumnType); ok && isWidening(s.From, s.FromLength, s.To, s.ToLength) && (s.From == "varchar") {
			return fmt.Sprintf(" (it widens %s.%s, and values longer than the old bound may exist now; to go back, change the field's tag and run makemigrations for an explicit narrowing migration, which checks the data first)", s.Table, s.Column)
		}
	}
	return ""
}

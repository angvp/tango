package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/angvp/tango/db"
)

// EnsureTrackingTable creates the tango_migrations table if it doesn't
// already exist.
func EnsureTrackingTable(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect) error {
	if err := checkDialect(dialect); err != nil {
		return err
	}
	pkClause := "PRIMARY KEY (app, name)"
	timestampType := pick(dialect, "TIMESTAMP", "TIMESTAMPTZ", "DATETIME(6)")
	nameType := pick(dialect, "TEXT", "TEXT", mysqlIndexableText)
	query := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS tango_migrations (app %s NOT NULL, name %s NOT NULL, applied_at %s NOT NULL, %s)%s",
		nameType, nameType, timestampType, pkClause, pick(dialect, "", "", mysqlTableOptions),
	)
	_, err := sqlDB.ExecContext(ctx, query)
	return err
}

// MigrationKey identifies one app-scoped migration row.
type MigrationKey struct {
	App  string
	Name string
}

// AppliedMigrations returns the set of applied app-scoped migration keys.
func AppliedMigrations(ctx context.Context, sqlDB *sql.DB) (map[MigrationKey]bool, error) {
	rows, err := sqlDB.QueryContext(ctx, "SELECT app, name FROM tango_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[MigrationKey]bool)
	for rows.Next() {
		var key MigrationKey
		if err := rows.Scan(&key.App, &key.Name); err != nil {
			return nil, err
		}
		applied[key] = true
	}
	return applied, rows.Err()
}

// IsMissingTrackingTable reports whether err means the tango_migrations table
// does not exist yet. Status uses this to stay read-only: creating the tracking
// table remains ApplyPending's responsibility.
func IsMissingTrackingTable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") ||
		strings.Contains(message, `relation "tango_migrations" does not exist`) ||
		strings.Contains(message, "tango_migrations' doesn't exist") // MySQL error 1146
}

// touchedApps returns the distinct, non-empty apps a migration's steps
// touch: just the migration's own App, since one Migration belongs to one
// app by construction (Diff groups steps by app before producing a
// Migration).
func touchedApps(m Migration) []string {
	if m.App == "" {
		return nil
	}
	return []string{m.App}
}

// ApplyPending applies every migration in migrations whose (App, Name) is
// not already recorded in tango_migrations, translating each Up step via
// ApplyStep and recording one tango_migrations row per (app, name) pair the
// migration touches. Slice order does not matter: pending migrations run by
// Name then App, except that a migration adding a foreign key to another
// app's table runs after the migration creating that table, so cross-app
// foreign keys work regardless of app names or InstalledApps order. Each
// app's own migrations always run in Name order. Foreign keys between apps
// that form a cycle are an error, and nothing is applied.
func ApplyPending(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, migrations []Migration) error {
	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		return err
	}

	applied, err := AppliedMigrations(ctx, sqlDB)
	if err != nil {
		return err
	}

	var pending []Migration
	for _, m := range migrations {
		if !applied[MigrationKey{App: m.App, Name: m.Name}] {
			pending = append(pending, m)
		}
	}
	ordered, err := applyOrder(pending)
	if err != nil {
		return err
	}

	for _, m := range ordered {
		steps := stepOrder(m, m.Up)
		if err := preflightMigration(ctx, sqlDB, dialect, steps); err != nil {
			return fmt.Errorf("migration %q: %w", m.Name, err)
		}
		for i, step := range steps {
			if err := applyStep(ctx, sqlDB, dialect, step, true); err != nil {
				return partialFailure(m.Name, steps, i, err)
			}
		}

		appliedAt := time.Now().UTC()
		for _, app := range touchedApps(m) {
			if _, err := sqlDB.ExecContext(ctx,
				trackingInsertSQL(dialect),
				app, m.Name, appliedAt,
			); err != nil {
				return fmt.Errorf("migration %q: recording tango_migrations row: %w", m.Name, err)
			}
		}
	}

	return nil
}

func trackingInsertSQL(dialect db.Dialect) string {
	return fmt.Sprintf(
		"INSERT INTO tango_migrations (app, name, applied_at) VALUES (%s, %s, %s)",
		placeholderAt(dialect, 1), placeholderAt(dialect, 2), placeholderAt(dialect, 3),
	)
}

func placeholderAt(dialect db.Dialect, n int) string {
	return pick(dialect, "?", fmt.Sprintf("$%d", n), "?")
}

// partialFailure is the error for a step that failed after earlier steps of
// the same migration were applied. Steps are not rolled back (MySQL commits
// each DDL statement as it runs, and the other dialects apply a migration's
// steps one by one too), and no tango_migrations row is written, so the error
// names exactly what was done: the schema needs repairing by hand before the
// migration can be run again. tanGO never resumes a half-applied migration.
func partialFailure(name string, steps []Step, failed int, err error) error {
	if failed == 0 {
		return fmt.Errorf("migration %q: %w", name, err)
	}
	applied := make([]string, failed)
	for i, step := range steps[:failed] {
		applied[i] = fmt.Sprintf("%d %T", i+1, step)
	}
	return fmt.Errorf("migration %q: step %d (%T) failed after %d earlier steps were applied and left in place (%s); no tango_migrations row was written, so repair the schema by hand before running the migration again: %w",
		name, failed+1, steps[failed], failed, strings.Join(applied, ", "), err)
}

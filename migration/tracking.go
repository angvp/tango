package migration

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/angvp/tango/db"
)

// EnsureTrackingTable creates the tango_migrations table if it doesn't
// already exist.
func EnsureTrackingTable(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect) error {
	pkClause := "PRIMARY KEY (app, name)"
	timestampType := "TIMESTAMP"
	if dialect == db.Postgres {
		timestampType = "TIMESTAMPTZ"
	}
	query := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS tango_migrations (app TEXT NOT NULL, name TEXT NOT NULL, applied_at %s NOT NULL, %s)",
		timestampType, pkClause,
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

// ApplyPending applies every migration in migrations whose Name is not
// already recorded in tango_migrations, in slice order, translating each
// Up step via ApplyStep and recording one tango_migrations row per
// (app, name) pair the migration touches.
func ApplyPending(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, migrations []Migration) error {
	if err := EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		return err
	}

	applied, err := AppliedMigrations(ctx, sqlDB)
	if err != nil {
		return err
	}

	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name == sorted[j].Name {
			return sorted[i].App < sorted[j].App
		}
		return sorted[i].Name < sorted[j].Name
	})

	for _, m := range sorted {
		key := MigrationKey{App: m.App, Name: m.Name}
		if applied[key] {
			continue
		}

		for _, step := range m.Up {
			if err := ApplyStep(ctx, sqlDB, dialect, step); err != nil {
				return fmt.Errorf("migration %q: %w", m.Name, err)
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
	if dialect == db.Postgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

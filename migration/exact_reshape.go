package migration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/angvp/tango/db"
)

// isReshape reports whether s changes only a column's integer width or time
// zone: the same base type at both ends.
func isReshape(s AlterColumnType) bool {
	return s.From == s.To && s.FromLength == s.ToLength &&
		(s.FromWidth != s.ToWidth || s.FromNaive != s.ToNaive)
}

// validateReshape allows the two lossless directions: a 4-byte integer
// becoming an 8-byte one, and a timestamp without time zone becoming one with
// it (its values read as UTC).
func validateReshape(s AlterColumnType) error {
	label := func(width int, naive bool) string {
		switch {
		case s.From == "timestamp" && naive:
			return "timestamp without time zone"
		case s.From == "timestamp":
			return "timestamp with time zone"
		}
		return widthLabel(width)
	}
	if s.FromWidth != s.ToWidth && !(s.FromWidth == int4Width && s.ToWidth == 0) ||
		s.FromNaive != s.ToNaive && !(s.FromNaive && !s.ToNaive) {
		return fmt.Errorf("tango migration: %s.%s: %s to %s is not a widening type change", s.Table, s.Column,
			label(s.FromWidth, s.FromNaive), label(s.ToWidth, s.ToNaive))
	}
	return nil
}

// alterColumnReshape widens a column's integer width or time zone. SQLite
// stores both shapes alike, so it has nothing to do.
func alterColumnReshape(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s AlterColumnType) error {
	if err := validateReshape(s); err != nil {
		return err
	}
	switch dialect {
	case db.SQLite:
		return nil
	case db.MySQL:
		return mysqlReshape(ctx, sqlDB, s)
	case db.Postgres:
		return postgresReshape(ctx, sqlDB, s)
	}
	return unsupportedDialect(dialect)
}

func postgresReshape(ctx context.Context, sqlDB *sql.DB, s AlterColumnType) error {
	table, column := quote(db.Postgres, s.Table), quote(db.Postgres, s.Column)
	if s.FromNaive && !s.ToNaive {
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE TIMESTAMPTZ USING %s AT TIME ZONE 'UTC'", table, column, column))
	}
	return inTx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE BIGINT USING %s", table, column, column)); err != nil {
			return err
		}
		// A SERIAL column's sequence is AS integer and would overflow the
		// widened column long before the column does.
		var sequence sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT pg_get_serial_sequence($1, $2)", quote(db.Postgres, s.Table), s.Column).Scan(&sequence); err != nil {
			return err
		}
		if !sequence.Valid {
			return nil
		}
		_, err := tx.ExecContext(ctx, "ALTER SEQUENCE "+sequence.String+" AS bigint")
		return err
	})
}

// mysqlReshape rewrites the column as BIGINT restating NOT NULL, the typed
// default and AUTO_INCREMENT, which MODIFY COLUMN would otherwise drop. A
// timestamp's two forms are the same DATETIME(6) here.
func mysqlReshape(ctx context.Context, sqlDB *sql.DB, s AlterColumnType) error {
	if s.FromWidth == s.ToWidth {
		return nil
	}
	info, err := mysqlColumn(ctx, sqlDB, s.Table, s.Column)
	if err != nil {
		return err
	}
	info.columnType = "BIGINT"
	return exec(ctx, sqlDB, mysqlModifyFull(s.Table, s.Column, info, mysqlRestate{
		notNull: !info.nullable, defaultKind: s.DefaultKind, defaultValue: s.DefaultValue,
	}))
}

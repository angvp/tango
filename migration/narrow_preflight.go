package migration

import (
	"context"
	"database/sql"
	"fmt"
	"unicode/utf8"

	"github.com/angvp/tango/db"
)

// preflightNarrowing fails, before any DDL, when a value in the column
// already exceeds the new bound. It streams the table's primary key and the
// value, counting runes the way Store validation does (utf8.RuneCountInString)
// instead of asking a dialect's length function, so SQLite and PostgreSQL
// answer alike. It reads only: nothing is truncated or rewritten.
func preflightNarrowing(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s AlterColumnType) error {
	pk, err := primaryKeyColumn(ctx, sqlDB, dialect, s.Table)
	if err != nil {
		return fmt.Errorf("tango migration: %s.%s: checking the data before narrowing: %w", s.Table, s.Column, err)
	}
	rows, err := sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IS NOT NULL ORDER BY %s",
		quote(dialect, pk), quote(dialect, s.Column), quote(dialect, s.Table), quote(dialect, s.Column), quote(dialect, pk)))
	if err != nil {
		return fmt.Errorf("tango migration: %s.%s: checking the data before narrowing: %w", s.Table, s.Column, err)
	}
	defer rows.Close()

	violations, first := 0, ""
	for rows.Next() {
		var key any
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("tango migration: %s.%s: checking the data before narrowing: %w", s.Table, s.Column, err)
		}
		if utf8.RuneCountInString(value) <= s.ToLength {
			continue
		}
		if violations == 0 {
			first = describeKey(pk, key)
		}
		violations++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("tango migration: %s.%s: checking the data before narrowing: %w", s.Table, s.Column, err)
	}
	if violations == 0 {
		return nil
	}
	noun := "rows hold"
	if violations == 1 {
		noun = "row holds"
	}
	return fmt.Errorf("tango migration: %s.%s: cannot narrow %s to %s: %d %s a longer value (lengths count characters), for example the row with %s; shorten or clean that data first, then run the migration again",
		s.Table, s.Column, typeLabel(s.From, s.FromLength), typeLabel(s.To, s.ToLength), violations, noun, first)
}

// primaryKeyColumn finds the table's own single primary-key column.
func primaryKeyColumn(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table string) (string, error) {
	query := "SELECT name FROM pragma_table_info($1) WHERE pk > 0"
	arg := table
	if dialect == db.Postgres {
		query = `SELECT a.attname FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
			WHERE i.indisprimary AND i.indrelid = $1::regclass`
		arg = quote(dialect, table)
	}
	rows, err := sqlDB.QueryContext(ctx, query, arg)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(names) != 1 {
		return "", fmt.Errorf("the table needs exactly one primary-key column to report an offending row, found %d", len(names))
	}
	return names[0], nil
}

// describeKey renders "column = value", quoting text keys.
func describeKey(column string, key any) string {
	switch v := key.(type) {
	case []byte:
		return fmt.Sprintf("%s = %q", column, string(v))
	case string:
		return fmt.Sprintf("%s = %q", column, v)
	default:
		return fmt.Sprintf("%s = %v", column, v)
	}
}

// preflightMigration checks every narrowing in steps against the database as
// it is now, before the migration runs any DDL, so a refused narrowing leaves
// the schema as well as the data unchanged. A step names its table and column
// as they are after the earlier steps, so earlier renames are undone to find
// the physical names; a narrowing of a table or column the migration itself
// creates has no existing data and is skipped.
func preflightMigration(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, steps []Step) error {
	for i, step := range steps {
		s, ok := step.(AlterColumnType)
		if !ok || !isNarrowing(s.From, s.FromLength, s.To, s.ToLength) {
			continue
		}
		table, column, created := physicalNames(steps[:i], s.Table, s.Column)
		if created {
			continue
		}
		s.Table, s.Column = table, column
		if err := preflightNarrowing(ctx, sqlDB, dialect, s); err != nil {
			return err
		}
	}
	return nil
}

// physicalNames walks earlier backwards from the names a later step uses to
// the names the database holds before any of them ran. created reports a
// table or column that one of them creates.
func physicalNames(earlier []Step, table, column string) (string, string, bool) {
	for j := len(earlier) - 1; j >= 0; j-- {
		switch e := earlier[j].(type) {
		case RenameTable:
			if table == e.To {
				table = e.From
			}
		case RenameColumn:
			if table == e.Table && column == e.To {
				column = e.From
			}
		case CreateTable:
			if table == e.Table {
				return table, column, true
			}
		case AddColumn:
			if table == e.Table && column == e.Column.Name {
				return table, column, true
			}
		}
	}
	return table, column, false
}

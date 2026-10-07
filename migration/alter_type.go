package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
)

// wideningKey names a Widening type change.
type wideningKey struct{ from, to string }

// wideningConversions are the only type changes AlterColumnType performs:
// each turns a column's value into the new type's value for every possible
// value, on both dialects alike, with NULL staying NULL. The expressions
// take the quoted column name.
var wideningConversions = map[wideningKey]map[db.Dialect]func(column string) string{
	{"integer", "real"}: {
		db.SQLite:   func(c string) string { return "CAST(" + c + " AS REAL)" },
		db.Postgres: func(c string) string { return c + "::double precision" },
	},
	{"integer", "text"}: {
		db.SQLite:   func(c string) string { return "CAST(" + c + " AS TEXT)" },
		db.Postgres: func(c string) string { return c + "::text" },
	},
	{"real", "text"}: {
		// SQLite writes 1.0 as "1.0" and 1e20 as "1.0e+20"; PostgreSQL
		// writes "1" and "1e+20". Dropping SQLite's ".0" before an exponent
		// or at the end gives PostgreSQL's form.
		db.SQLite: func(c string) string {
			text := "CAST(" + c + " AS TEXT)"
			return "CASE WHEN " + text + " LIKE '%.0' THEN substr(" + text + ", 1, length(" + text + ") - 2) ELSE replace(" + text + ", '.0e', 'e') END"
		},
		db.Postgres: func(c string) string { return c + "::text" },
	},
	{"boolean", "integer"}: {
		db.SQLite:   booleanTo("1", "0"),
		db.Postgres: booleanTo("1", "0"),
	},
	{"boolean", "text"}: {
		db.SQLite:   booleanTo("'true'", "'false'"),
		db.Postgres: booleanTo("'true'", "'false'"),
	},
}

// booleanTo converts a boolean with an explicit CASE, never a cast:
// PostgreSQL has no implicit boolean to integer cast, and SQLite stores
// booleans as 1 and 0.
func booleanTo(whenTrue, whenFalse string) func(string) string {
	return func(c string) string {
		return "CASE WHEN " + c + " THEN " + whenTrue + " WHEN NOT " + c + " THEN " + whenFalse + " END"
	}
}

// isWideningTypeChange reports whether a column of type from can become
// type to with every existing value converting.
func isWideningTypeChange(from, to string) bool {
	_, ok := wideningConversions[wideningKey{from, to}]
	return ok
}

// alterColumnType changes s.Column's type in one statement on PostgreSQL
// and one transactional rebuild on SQLite, so a failure leaves the column,
// its values and its default as they were.
func alterColumnType(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s AlterColumnType) error {
	conversions, ok := wideningConversions[wideningKey{s.From, s.To}]
	if !ok {
		return fmt.Errorf("tango migration: %s.%s: %s to %s is not a widening type change", s.Table, s.Column, s.From, s.To)
	}
	convert := conversions[dialect]
	if dialect == db.Postgres {
		column := quote(dialect, s.Column)
		actions := []string{fmt.Sprintf("ALTER COLUMN %s TYPE %s USING %s", column, baseTypeSQL(dialect, s.To), convert(column))}
		if s.Default != "" {
			actions = append([]string{"ALTER COLUMN " + column + " DROP DEFAULT"}, actions...)
			actions = append(actions, "ALTER COLUMN "+column+" SET DEFAULT "+s.Default)
		}
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s %s", quote(dialect, s.Table), strings.Join(actions, ", ")))
	}
	return rebuildSQLiteTable(ctx, sqlDB, s.Table, func(columns []sqliteColumn) ([]sqliteColumn, error) {
		reshaped := make([]sqliteColumn, len(columns))
		copy(reshaped, columns)
		for i, c := range reshaped {
			if c.name != s.Column {
				continue
			}
			reshaped[i].declType = baseTypeSQL(dialect, s.To)
			reshaped[i].source = convert(quote(dialect, c.name))
			if s.Default != "" {
				reshaped[i].defaultValue = sql.NullString{String: s.Default, Valid: true}
			}
			return reshaped, nil
		}
		return nil, fmt.Errorf("tango migration: column %q does not exist on table %q", s.Column, s.Table)
	})
}

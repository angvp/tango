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

// wideningConversion is one Widening type change: how each dialect
// converts a column's values, and how a default literal converts the same
// way.
type wideningConversion struct {
	// sql takes the quoted column name and returns the converting
	// expression, NULL staying NULL.
	sql map[db.Dialect]func(column string) string
	// convertDefault converts a raw SQL default literal of the old type,
	// failing for anything that is not one.
	convertDefault func(def string) (string, bool)
}

// wideningConversions are the only type changes AlterColumnType performs:
// each turns every possible value of the old type into the new type's
// value, on both dialects alike.
var wideningConversions = map[wideningKey]wideningConversion{
	{"integer", "real"}: {
		sql: map[db.Dialect]func(string) string{
			db.SQLite:   func(c string) string { return "CAST(" + c + " AS REAL)" },
			db.Postgres: func(c string) string { return c + "::double precision" },
		},
		convertDefault: integerDefault(func(def string) string { return def + ".0" }),
	},
	{"integer", "text"}: {
		sql: map[db.Dialect]func(string) string{
			db.SQLite:   func(c string) string { return "CAST(" + c + " AS TEXT)" },
			db.Postgres: func(c string) string { return c + "::text" },
		},
		convertDefault: integerDefault(func(def string) string { return "'" + def + "'" }),
	},
	{"real", "text"}: {
		sql: map[db.Dialect]func(string) string{
			// SQLite's own text form differs from PostgreSQL's ("1.0",
			// "1000000000000000.0" where PostgreSQL writes "1", "1e+15"), so
			// the rebuild rewrites each value with canonicalRealText after
			// this copy (see writeCanonicalRealText).
			db.SQLite:   func(c string) string { return "CAST(" + c + " AS TEXT)" },
			db.Postgres: func(c string) string { return c + "::text" },
		},
		convertDefault: realToTextDefault,
	},
	// A bounded string becomes text, or a longer bounded string: every value
	// already fits, so the column's values are copied as they are. The
	// longer-only rule for varchar to varchar lives in isWidening.
	{"varchar", "text"}:    unchangedValues,
	{"varchar", "varchar"}: unchangedValues,
	{"boolean", "integer"}: {
		sql: map[db.Dialect]func(string) string{
			db.SQLite:   booleanTo("1", "0"),
			db.Postgres: booleanTo("1", "0"),
		},
		convertDefault: booleanDefault("1", "0"),
	},
	{"boolean", "text"}: {
		sql: map[db.Dialect]func(string) string{
			db.SQLite:   booleanTo("'true'", "'false'"),
			db.Postgres: booleanTo("'true'", "'false'"),
		},
		convertDefault: booleanDefault("'true'", "'false'"),
	},
}

var unchangedValues = wideningConversion{
	sql: map[db.Dialect]func(string) string{
		db.SQLite:   func(c string) string { return c },
		db.Postgres: func(c string) string { return c },
	},
	convertDefault: func(def string) (string, bool) { return def, true },
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

// isWidening is isWideningTypeChange for typed columns: a bounded string
// only widens to a strictly longer bound, and to text.
func isWidening(from string, fromLength int, to string, toLength int) bool {
	if from == "varchar" && to == "varchar" {
		return toLength > fromLength
	}
	return isWideningTypeChange(from, to)
}

// typeLabel names a column type for messages, with a bounded string's length.
func typeLabel(columnType string, length int) string {
	if columnType == "varchar" {
		return fmt.Sprintf("varchar(%d)", length)
	}
	return columnType
}

// alterColumnType changes s.Column's type in one statement on PostgreSQL
// and one transactional rebuild on SQLite, so a failure leaves the column,
// its values and its default as they were.
func alterColumnType(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s AlterColumnType) error {
	if err := validateAlterLengths(s); err != nil {
		return err
	}
	conversion, ok := wideningConversions[wideningKey{s.From, s.To}]
	if !ok || !isWidening(s.From, s.FromLength, s.To, s.ToLength) {
		return fmt.Errorf("tango migration: %s.%s: %s to %s is not a widening type change", s.Table, s.Column, typeLabel(s.From, s.FromLength), typeLabel(s.To, s.ToLength))
	}
	convert := conversion.sql[dialect]
	if dialect == db.Postgres {
		column := quote(dialect, s.Column)
		actions := []string{fmt.Sprintf("ALTER COLUMN %s TYPE %s USING %s", column, baseTypeSQL(dialect, s.To, s.ToLength), convert(column))}
		if s.Default != "" {
			actions = append([]string{"ALTER COLUMN " + column + " DROP DEFAULT"}, actions...)
			actions = append(actions, "ALTER COLUMN "+column+" SET DEFAULT "+s.Default)
		}
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s %s", quote(dialect, s.Table), strings.Join(actions, ", ")))
	}
	var afterCopy copyFixup
	if s.From == "real" && s.To == "text" {
		afterCopy = writeCanonicalRealText(s.Column)
	}
	return rebuildSQLiteTable(ctx, sqlDB, s.Table, func(columns []sqliteColumn) ([]sqliteColumn, error) {
		reshaped := make([]sqliteColumn, len(columns))
		copy(reshaped, columns)
		for i, c := range reshaped {
			if c.name != s.Column {
				continue
			}
			reshaped[i].declType = baseTypeSQL(dialect, s.To, s.ToLength)
			reshaped[i].source = convert(quote(dialect, c.name))
			if s.Default != "" {
				reshaped[i].defaultValue = sql.NullString{String: s.Default, Valid: true}
			}
			return reshaped, nil
		}
		return nil, fmt.Errorf("tango migration: column %q does not exist on table %q", s.Column, s.Table)
	}, afterCopy)
}

// writeCanonicalRealText rewrites column in the rebuilt table with each old
// row's real value in PostgreSQL's text form, which SQLite's CAST does not
// produce. Rows are matched by the table's primary key.
func writeCanonicalRealText(column string) copyFixup {
	return func(ctx context.Context, tx *sql.Tx, oldTable, newTable string, columns []sqliteColumn) error {
		pk := ""
		for _, c := range columns {
			if c.primaryKey {
				pk = c.name
			}
		}
		if pk == "" {
			return fmt.Errorf("tango migration: %s.%s: changing real to text on SQLite needs a primary key", oldTable, column)
		}
		rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IS NOT NULL",
			quote(db.SQLite, pk), quote(db.SQLite, column), quote(db.SQLite, oldTable), quote(db.SQLite, column)))
		if err != nil {
			return err
		}
		type converted struct {
			key  any
			text string
		}
		var values []converted
		for rows.Next() {
			var key any
			var value float64
			if err := rows.Scan(&key, &value); err != nil {
				rows.Close()
				return err
			}
			values = append(values, converted{key, canonicalRealText(value)})
		}
		if err := rows.Close(); err != nil {
			return err
		}
		update := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2", quote(db.SQLite, newTable), quote(db.SQLite, column), quote(db.SQLite, pk))
		for _, v := range values {
			if _, err := tx.ExecContext(ctx, update, v.text, v.key); err != nil {
				return err
			}
		}
		return nil
	}
}

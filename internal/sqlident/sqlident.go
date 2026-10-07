// Package sqlident quotes SQL identifiers (table, column and index names)
// for the SQL tanGO generates, so a model or field named after a reserved
// word ("user", "order", "group") works on every dialect.
//
// It is the one quoting helper shared by db.Store and the migration DDL.
// It takes a Style rather than a db.Dialect because db itself imports it;
// each package maps its dialect to a Style in one place.
//
// The two styles differ on purpose. PostgreSQL only accepts the standard
// double quote. SQLite accepts double quotes too, but falls back to reading
// a double-quoted name that matches no column as a string literal, so a
// missing column would silently select its own name instead of failing;
// SQLite's backtick quoting has no such fallback.
//
// Quoting never changes an identifier's identity for the names tanGO
// generates: they are lower-case (see db.ColumnName), which is what
// PostgreSQL folds an unquoted name to, and SQLite matches names
// case-insensitively whether quoted or not.
package sqlident

import "strings"

// Style selects a dialect's identifier quote character.
type Style int

const (
	// Backtick quotes as `name`, escaping ` as ``. Used for SQLite.
	Backtick Style = iota
	// DoubleQuote quotes as "name", escaping " as "". Used for PostgreSQL.
	DoubleQuote
)

// Quote returns name as a quoted identifier in style.
func Quote(style Style, name string) string {
	q := "`"
	if style == DoubleQuote {
		q = `"`
	}
	return q + strings.ReplaceAll(name, q, q+q) + q
}

// QuoteAll quotes each name in style and joins them with ", ".
func QuoteAll(style Style, names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = Quote(style, name)
	}
	return strings.Join(quoted, ", ")
}

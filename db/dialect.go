package db

import "strconv"

// Dialect selects which SQL dialect Store generates statements for.
type Dialect int

const (
	// SQLite generates SQL using "?" placeholders and backfills
	// database-generated primary keys via sql.Result.LastInsertId.
	SQLite Dialect = iota
	// Postgres generates SQL using "$N" placeholders and backfills
	// database-generated primary keys via INSERT ... RETURNING.
	Postgres
)

// placeholder returns the parameter placeholder for the n-th (1-indexed)
// argument in a statement, in d's syntax.
func placeholder(d Dialect, n int) string {
	if d == Postgres {
		return "$" + strconv.Itoa(n)
	}
	return "?"
}

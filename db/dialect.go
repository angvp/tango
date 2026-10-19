package db

import (
	"fmt"
	"strconv"
)

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

// checkDialect panics on a Dialect no release defines. Every branch on a
// Dialect is a switch with a panicking default, so a value this package does
// not know can never fall through to another dialect's SQL.
func checkDialect(d Dialect) {
	switch d {
	case SQLite, Postgres:
		return
	}
	panic(unsupportedDialect(d))
}

func unsupportedDialect(d Dialect) string {
	return fmt.Sprintf("tango db: unsupported dialect %d", int(d))
}

// placeholder returns the parameter placeholder for the n-th (1-indexed)
// argument in a statement, in d's syntax.
func placeholder(d Dialect, n int) string {
	switch d {
	case SQLite:
		return "?"
	case Postgres:
		return "$" + strconv.Itoa(n)
	}
	panic(unsupportedDialect(d))
}

// returnsInsertedKey reports whether d backfills a generated primary key with
// INSERT ... RETURNING (true) or sql.Result.LastInsertId (false).
func (d Dialect) returnsInsertedKey() bool {
	switch d {
	case SQLite:
		return false
	case Postgres:
		return true
	}
	panic(unsupportedDialect(d))
}

// offsetNeedsLimit reports whether d refuses OFFSET without LIMIT.
func (d Dialect) offsetNeedsLimit() bool {
	switch d {
	case SQLite:
		return true
	case Postgres:
		return false
	}
	panic(unsupportedDialect(d))
}

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
	// MySQL generates SQL using "?" placeholders and backticked identifiers,
	// and backfills database-generated primary keys via
	// sql.Result.LastInsertId.
	MySQL
)

// checkDialect panics on a Dialect no release defines. Every branch on a
// Dialect is a switch with a panicking default, so a value this package does
// not know can never fall through to another dialect's SQL.
func checkDialect(d Dialect) {
	switch d {
	case SQLite, Postgres, MySQL:
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
	case SQLite, MySQL:
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
	case SQLite, MySQL:
		return false
	case Postgres:
		return true
	}
	panic(unsupportedDialect(d))
}

// offsetNeedsLimit reports whether d refuses OFFSET without LIMIT.
func (d Dialect) offsetNeedsLimit() bool {
	switch d {
	case SQLite, MySQL:
		return true
	case Postgres:
		return false
	}
	panic(unsupportedDialect(d))
}

// noLimit is the LIMIT value that means "no limit", for the dialects that
// need a LIMIT to accept an OFFSET.
func (d Dialect) noLimit() string {
	switch d {
	case SQLite:
		return "-1"
	case MySQL:
		return "18446744073709551615" // the largest unsigned BIGINT, MySQL's documented idiom
	case Postgres:
		panic("tango db: PostgreSQL accepts OFFSET without LIMIT")
	}
	panic(unsupportedDialect(d))
}

// implicitStringBound is the most runes MySQL can index in a string column
// whose model gives no bound of its own. MySQL cannot index unbounded text,
// so a unique or indexed bare string is VARCHAR(255) there; Store refuses a
// longer value with *ValueTooLongError before any SQL. Other dialects index
// text as it is and have no such bound.
func (d Dialect) implicitStringBound() int {
	switch d {
	case MySQL:
		return 255
	case SQLite, Postgres:
		return 0
	}
	panic(unsupportedDialect(d))
}

package db

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// mysqlDuplicateEntry is MySQL's error number for a unique-key violation
// (ER_DUP_ENTRY).
const mysqlDuplicateEntry = 1062

// IsUniqueConstraintViolation reports whether err is a unique-constraint
// violation returned by Create or Update, on any supported dialect.
//
// This exists so a caller can attempt a write directly and treat the
// database's own unique constraint as the actual correctness boundary for
// a duplicate value, rather than a SELECT-then-INSERT pre-check — which
// only narrows, and never closes, the race between two concurrent writers
// for the same unique value. A pre-check may still exist purely for a
// faster/friendlier error on the common non-racing path, but correctness
// must not depend on it.
func IsUniqueConstraintViolation(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDuplicateEntry
	}

	// modernc.org/sqlite does not expose a typed error for this; its error
	// message is the only signal available.
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

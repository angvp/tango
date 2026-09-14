package db

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueConstraintViolation reports whether err is a unique-constraint
// violation returned by Create or Update, on either supported dialect.
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

	// modernc.org/sqlite does not expose a typed error for this; its error
	// message is the only signal available.
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

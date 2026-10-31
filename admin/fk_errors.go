package admin

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// isForeignKeyViolation reports whether the database refused a write because
// a foreign key would be left dangling (a referenced row deleted, or a
// referenced row missing on insert or update), on any supported dialect.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		// ER_ROW_IS_REFERENCED_2 and ER_NO_REFERENCED_ROW_2.
		return myErr.Number == 1451 || myErr.Number == 1452
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

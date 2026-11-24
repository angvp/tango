package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/angvp/tango/db"
)

// MySQL errors for an index key that is too long: the key exceeds the
// maximum length (1071), and a TEXT column used in a key without a length
// (1170). Only these two are wrapped; every other error is the database's own.
const (
	mysqlErrTooLongKey      = 1071
	mysqlErrTextKeyNoLength = 1170
)

// createCompositeIndexSQL returns "CREATE [UNIQUE] INDEX name ON table (c1, c2)"
// with every identifier quoted.
func createCompositeIndexSQL(dialect db.Dialect, unique bool, name, table string, columns []string) string {
	verb := "CREATE INDEX"
	if unique {
		verb = "CREATE UNIQUE INDEX"
	}
	quoted := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = quote(dialect, c)
	}
	return fmt.Sprintf("%s %s ON %s (%s)", verb, quote(dialect, name), quote(dialect, table), strings.Join(quoted, ", "))
}

func checkCompositeStep(table string, columns []string, name string) error {
	if len(columns) < 2 {
		return fmt.Errorf("tango migration: an index on table %q needs at least two columns, got %d", table, len(columns))
	}
	if name != "" {
		return validateIndexName(table, columns[0], name)
	}
	return nil
}

func createCompositeIndex(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s CreateCompositeIndex) error {
	if err := checkCompositeStep(s.Table, s.Columns, s.Name); err != nil {
		return err
	}
	for _, column := range s.Columns {
		if err := ensureIndexable(ctx, sqlDB, dialect, s.Table, column); err != nil {
			return err
		}
	}
	name := compositeIndexName(s.Table, CompositeIndex{Columns: s.Columns, Unique: s.Unique, Name: s.Name})
	err := exec(ctx, sqlDB, createCompositeIndexSQL(dialect, s.Unique, name, s.Table, s.Columns))
	return wrapKeyTooLong(err, s.Table, name, s.Columns)
}

func dropCompositeIndex(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s DropCompositeIndex) error {
	if err := checkCompositeStep(s.Table, s.Columns, s.Name); err != nil {
		return err
	}
	name := compositeIndexName(s.Table, CompositeIndex{Columns: s.Columns, Unique: s.Unique, Name: s.Name})
	return exec(ctx, sqlDB, dropIndexSQL(dialect, name, s.Table))
}

// wrapKeyTooLong names the table, index, columns and the way out when MySQL
// refuses an index key as too long. Any other error passes through.
func wrapKeyTooLong(err error, table, name string, columns []string) error {
	var myErr *mysql.MySQLError
	if !errors.As(err, &myErr) || (myErr.Number != mysqlErrTooLongKey && myErr.Number != mysqlErrTextKeyNoLength) {
		return err
	}
	return fmt.Errorf("tango migration: index %q on table %q cannot cover columns (%s): MySQL refuses a key this long; "+
		"declare the text columns with varchar=n so the key fits: %w", name, table, strings.Join(columns, ", "), err)
}

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

func renameCompositeIndex(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s RenameCompositeIndex) error {
	if len(s.Columns) < 2 {
		return fmt.Errorf("tango migration: an index on table %q needs at least two columns, got %d", s.Table, len(s.Columns))
	}
	for _, name := range []string{s.From, s.To} {
		if err := validateIndexName(s.Table, s.Columns[0], name); err != nil {
			return err
		}
	}
	switch dialect {
	case db.Postgres:
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER INDEX %s RENAME TO %s", quote(dialect, s.From), quote(dialect, s.To)))
	case db.MySQL:
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s RENAME INDEX %s TO %s", quote(dialect, s.Table), quote(dialect, s.From), quote(dialect, s.To)))
	case db.SQLite:
		// SQLite has no rename for an index: create it again under the new
		// name, from the table and columns the step carries.
		return inTx(ctx, sqlDB, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, dropIndexSQL(dialect, s.From, s.Table)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, createCompositeIndexSQL(dialect, s.Unique, s.To, s.Table, s.Columns))
			return err
		})
	}
	return unsupportedDialect(dialect)
}

// compositeIndexHolding returns the name of a multi-column index of table that
// holds column, or "" when there is none. A primary key and the index a foreign
// key makes for itself are not indexes a migration created, so they do not count.
func compositeIndexHolding(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, dialect db.Dialect, table, column string) (string, error) {
	var query string
	switch dialect {
	case db.Postgres:
		query = `SELECT c.relname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY (i.indkey)
			WHERE t.relname = $1 AND n.nspname = current_schema() AND a.attname = $2 AND i.indnatts > 1 AND NOT i.indisprimary
			ORDER BY c.relname LIMIT 1`
	case db.MySQL:
		query = `SELECT index_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name <> 'PRIMARY'
			AND index_name IN (SELECT index_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?)
			GROUP BY index_name HAVING COUNT(*) > 1 ORDER BY index_name LIMIT 1`
		return scanName(q.QueryRowContext(ctx, query, table, table, column))
	default:
		return "", unsupportedDialect(dialect)
	}
	return scanName(q.QueryRowContext(ctx, query, table, column))
}

func scanName(row *sql.Row) (string, error) {
	var name string
	if err := row.Scan(&name); errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return name, nil
}

func errColumnInIndex(table, column, index string) error {
	return fmt.Errorf("tango migration: cannot drop column %q of table %q: index %q still contains it; drop the index first", column, table, index)
}

// refuseDropOfIndexedColumn is DropColumn's guard on PostgreSQL and MySQL,
// which would otherwise drop the index silently or shrink it.
func refuseDropOfIndexedColumn(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table, column string) error {
	if dialect != db.Postgres && dialect != db.MySQL {
		return nil
	}
	name, err := compositeIndexHolding(ctx, sqlDB, dialect, table, column)
	if err != nil {
		return err
	}
	if name != "" {
		return errColumnInIndex(table, column, name)
	}
	return nil
}

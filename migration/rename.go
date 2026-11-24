package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
)

// renameColumn renames the column natively on both dialects and renames the
// indexes named after it, in one transaction, so a later step that names
// an index by its column (DropIndex, AlterColumnUnique) finds it.
func renameColumn(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s RenameColumn) error {
	return inTx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
			quote(dialect, s.Table), quote(dialect, s.From), quote(dialect, s.To))); err != nil {
			return err
		}
		for _, index := range []struct {
			from, to string
			unique   bool
		}{
			{indexName(s.Table, s.From), indexName(s.Table, s.To), false},
			{uniqueIndexName(s.Table, s.From), uniqueIndexName(s.Table, s.To), true},
		} {
			if err := renameIndex(ctx, tx, dialect, s.Table, s.To, index.from, index.to, index.unique); err != nil {
				return err
			}
		}
		return nil
	})
}

// renameIndex renames index from to to if it exists. SQLite has no
// ALTER INDEX ... RENAME, so it drops the index and creates it again on
// column.
func renameIndex(ctx context.Context, tx *sql.Tx, dialect db.Dialect, table, column, from, to string, unique bool) error {
	if checkDialect(dialect) != nil {
		return unsupportedDialect(dialect)
	}
	if dialect == db.Postgres {
		_, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER INDEX IF EXISTS %s RENAME TO %s", quote(dialect, from), quote(dialect, to)))
		return err
	}
	if dialect == db.MySQL {
		return mysqlRenameIndex(ctx, tx, table, from, to)
	}
	var name string
	err := tx.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'index' AND name = $1", from).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP INDEX %s", quote(dialect, from))); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, createIndexSQL(dialect, unique, to, table, column))
	return err
}

// renameTable renames the table natively on both dialects (both carry
// other tables' foreign keys over to the new name) and renames the indexes
// named after it, in one transaction, so a later step that names an index
// by its table and column finds it.
func renameTable(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s RenameTable) error {
	return inTx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quote(dialect, s.From), quote(dialect, s.To))); err != nil {
			return err
		}
		names, err := tableSingleColumnIndexNames(ctx, tx, dialect, s.To)
		if err != nil {
			return err
		}
		for _, name := range names {
			for _, kind := range []struct {
				prefix string
				unique bool
			}{{"idx_", false}, {"uniq_", true}} {
				column, ok := strings.CutPrefix(name, kind.prefix+s.From+"_")
				if !ok {
					continue
				}
				if err := renameIndex(ctx, tx, dialect, s.To, column, name, kind.prefix+s.To+"_"+column, kind.unique); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// tableSingleColumnIndexNames lists the indexes created on table that cover
// one column. A multi-column index is named from its columns, not a column, and
// its own RenameCompositeIndex step renames it.
func tableSingleColumnIndexNames(ctx context.Context, tx *sql.Tx, dialect db.Dialect, table string) ([]string, error) {
	names, err := tableIndexNames(ctx, tx, dialect, table)
	if err != nil {
		return nil, err
	}
	single := names[:0:0]
	for _, name := range names {
		columns, err := indexColumnCount(ctx, tx, dialect, table, name)
		if err != nil {
			return nil, err
		}
		if columns == 1 {
			single = append(single, name)
		}
	}
	return single, nil
}

// indexColumnCount is the number of columns the named index covers.
func indexColumnCount(ctx context.Context, tx *sql.Tx, dialect db.Dialect, table, index string) (int, error) {
	var n int
	var err error
	switch dialect {
	case db.Postgres:
		err = tx.QueryRowContext(ctx, `SELECT i.indnatts FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relname = $1 AND n.nspname = current_schema()`, index).Scan(&n)
	case db.MySQL:
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, table, index).Scan(&n)
	default:
		columns, cerr := sqliteIndexColumns(ctx, tx, index)
		return len(columns), cerr
	}
	return n, err
}

// tableIndexNames lists the indexes created on table.
func tableIndexNames(ctx context.Context, tx *sql.Tx, dialect db.Dialect, table string) ([]string, error) {
	query := sqliteCreatedIndexesSQL
	if checkDialect(dialect) != nil {
		return nil, unsupportedDialect(dialect)
	}
	if dialect == db.Postgres {
		query = "SELECT indexname, '' FROM pg_indexes WHERE schemaname = current_schema() AND tablename = $1"
	}
	if dialect == db.MySQL {
		query = "SELECT DISTINCT index_name, '' FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ?"
	}
	rows, err := tx.QueryContext(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name, createSQL string
		if err := rows.Scan(&name, &createSQL); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// txBeginner is a *sql.DB or a *sql.Conn.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// inTx runs fn in a transaction on db, committing if fn succeeds and rolling
// back otherwise, so a step of several statements never stops halfway.
func inTx(ctx context.Context, db txBeginner, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

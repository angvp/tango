package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/angvp/tango/db"
)

// renameColumn renames the column natively on both dialects and renames the
// indexes named after it, in one transaction, so a later step that names
// an index by its column (DropIndex, AlterColumnUnique) finds it.
func renameColumn(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s RenameColumn) (err error) {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

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
	return tx.Commit()
}

// renameIndex renames index from to to if it exists. SQLite has no
// ALTER INDEX ... RENAME, so it drops the index and creates it again on
// column.
func renameIndex(ctx context.Context, tx *sql.Tx, dialect db.Dialect, table, column, from, to string, unique bool) error {
	if dialect == db.Postgres {
		_, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER INDEX IF EXISTS %s RENAME TO %s", quote(dialect, from), quote(dialect, to)))
		return err
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

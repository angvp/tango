package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
)

// explicitOr is name, or the derived name when name is empty.
func explicitOr(name, derived string) string {
	if name != "" {
		return name
	}
	return derived
}

// nullCount counts the rows of table whose column is NULL.
func nullCount(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table, column string) (int, error) {
	var n int
	err := sqlDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NULL",
		quote(dialect, table), quote(dialect, column))).Scan(&n)
	return n, err
}

// preflightNotNull fails, before any DDL, when a row already holds NULL in
// the column. It reads only; nothing is rewritten.
func preflightNotNull(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table, column string) error {
	n, err := nullCount(ctx, sqlDB, dialect, table, column)
	if err != nil {
		return fmt.Errorf("tango migration: %s.%s: checking the data before NOT NULL: %w", table, column, err)
	}
	if n == 0 {
		return nil
	}
	noun := "rows hold"
	if n == 1 {
		noun = "row holds"
	}
	return fmt.Errorf("tango migration: %s.%s: cannot set NOT NULL: %d %s NULL; fill that column first, then run the migration again", table, column, n, noun)
}

func alterColumnNotNull(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s AlterColumnNotNull, dataChecked bool) error {
	if err := validateTypedDefault(s.Table, s.Column, s.DefaultKind, s.DefaultValue); err != nil {
		return err
	}
	if s.NotNull && !dataChecked {
		if err := preflightNotNull(ctx, sqlDB, dialect, s.Table, s.Column); err != nil {
			return err
		}
	}
	switch dialect {
	case db.Postgres:
		action := pick(dialect, "", "DROP NOT NULL", "")
		if s.NotNull {
			action = "SET NOT NULL"
		}
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", quote(dialect, s.Table), quote(dialect, s.Column), action))
	case db.MySQL:
		return mysqlRestateColumn(ctx, sqlDB, s.Table, s.Column, func(info mysqlColumnInfo) (mysqlRestate, error) {
			if info.hasDefault && s.DefaultKind == "" {
				return mysqlRestate{}, fmt.Errorf("tango migration: %s.%s: has a default the step does not carry, which MODIFY COLUMN would drop", s.Table, s.Column)
			}
			return mysqlRestate{notNull: s.NotNull, defaultKind: s.DefaultKind, defaultValue: s.DefaultValue}, nil
		})
	case db.SQLite:
		return rebuildSQLiteTable(ctx, sqlDB, s.Table, func(columns []sqliteColumn) ([]sqliteColumn, error) {
			return reshapeSQLiteColumn(columns, s.Table, s.Column, func(c *sqliteColumn) { c.notNull = s.NotNull })
		}, nil)
	}
	return unsupportedDialect(dialect)
}

func alterColumnDefault(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s AlterColumnDefault) error {
	if err := validateTypedDefault(s.Table, s.Column, s.DefaultKind, s.DefaultValue); err != nil {
		return err
	}
	switch dialect {
	case db.Postgres:
		action := "DROP DEFAULT"
		if s.DefaultKind != "" {
			action = "SET DEFAULT " + defaultSQL(dialect, "", s.DefaultKind, s.DefaultValue)
		}
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", quote(dialect, s.Table), quote(dialect, s.Column), action))
	case db.MySQL:
		return mysqlAlterDefault(ctx, sqlDB, s)
	case db.SQLite:
		return rebuildSQLiteTable(ctx, sqlDB, s.Table, func(columns []sqliteColumn) ([]sqliteColumn, error) {
			return reshapeSQLiteColumn(columns, s.Table, s.Column, func(c *sqliteColumn) {
				c.defaultValue = sql.NullString{}
				if s.DefaultKind != "" {
					c.defaultValue = sql.NullString{String: defaultSQL(db.SQLite, "", s.DefaultKind, s.DefaultValue), Valid: true}
				}
			})
		}, nil)
	}
	return unsupportedDialect(dialect)
}

// reshapeSQLiteColumn applies change to the named column of a rebuild.
func reshapeSQLiteColumn(columns []sqliteColumn, table, column string, change func(*sqliteColumn)) ([]sqliteColumn, error) {
	reshaped := make([]sqliteColumn, len(columns))
	copy(reshaped, columns)
	for i := range reshaped {
		if reshaped[i].name == column {
			change(&reshaped[i])
			return reshaped, nil
		}
	}
	return nil, fmt.Errorf("tango migration: column %q does not exist on table %q", column, table)
}

// mysqlRestate is what a MODIFY COLUMN must say again because MySQL drops it.
type mysqlRestate struct {
	notNull      bool
	defaultKind  string
	defaultValue string
}

// mysqlRestateColumn rewrites a column with MODIFY COLUMN, restating its full
// type, NOT NULL, typed default and AUTO_INCREMENT as chosen by decide.
func mysqlRestateColumn(ctx context.Context, sqlDB *sql.DB, table, column string, decide func(mysqlColumnInfo) (mysqlRestate, error)) error {
	info, err := mysqlColumn(ctx, sqlDB, table, column)
	if err != nil {
		return err
	}
	restate, err := decide(info)
	if err != nil {
		return err
	}
	return exec(ctx, sqlDB, mysqlModifyFull(table, column, info, restate))
}

func mysqlModifyFull(table, column string, info mysqlColumnInfo, r mysqlRestate) string {
	statement := fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s", quote(db.MySQL, table), quote(db.MySQL, column), info.columnType)
	if r.notNull {
		statement += " NOT NULL"
	}
	if r.defaultKind != "" {
		statement += " DEFAULT " + defaultSQL(db.MySQL, strings.ToUpper(info.columnType), r.defaultKind, r.defaultValue)
	}
	if info.autoIncrement {
		statement += " AUTO_INCREMENT"
	}
	return statement
}

// mysqlAlterDefault sets or drops a column's typed default. MySQL refuses a
// parenthesized default in ALTER COLUMN SET DEFAULT on a TEXT column (error
// 1101), so a TEXT column takes MODIFY COLUMN, which accepts it. Dropping a
// default also takes MODIFY COLUMN: ALTER COLUMN DROP DEFAULT leaves a
// nullable column with no default at all, so an insert that omits it fails
// (error 1364), where a column created without one has DEFAULT NULL.
func mysqlAlterDefault(ctx context.Context, sqlDB *sql.DB, s AlterColumnDefault) error {
	info, err := mysqlColumn(ctx, sqlDB, s.Table, s.Column)
	if err != nil {
		return err
	}
	isText := strings.HasSuffix(strings.ToUpper(info.columnType), "TEXT")
	if s.DefaultKind != "" && !isText {
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", quote(db.MySQL, s.Table), quote(db.MySQL, s.Column),
			defaultSQL(db.MySQL, strings.ToUpper(info.columnType), s.DefaultKind, s.DefaultValue)))
	}
	return exec(ctx, sqlDB, mysqlModifyFull(s.Table, s.Column, info, mysqlRestate{notNull: !info.nullable, defaultKind: s.DefaultKind, defaultValue: s.DefaultValue}))
}

func renameIndexStep(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, s RenameIndex) error {
	for _, name := range []string{s.From, s.To} {
		if err := validateIndexName(s.Table, s.Column, name); err != nil {
			return err
		}
	}
	switch dialect {
	case db.Postgres:
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER INDEX %s RENAME TO %s", quote(dialect, s.From), quote(dialect, s.To)))
	case db.MySQL:
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s RENAME INDEX %s TO %s", quote(dialect, s.Table), quote(dialect, s.From), quote(dialect, s.To)))
	case db.SQLite:
		return inTx(ctx, sqlDB, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, dropIndexSQL(dialect, s.From, s.Table)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, createIndexSQL(dialect, s.Unique, s.To, s.Table, s.Column))
			return err
		})
	}
	return unsupportedDialect(dialect)
}

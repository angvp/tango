package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/angvp/tango/db"
)

// mysqlTableOptions makes every table InnoDB, utf8mb4, and case-sensitive
// (utf8mb4_bin), so a unique column and an equality test treat "Alice" and
// "alice" as different values, as SQLite and PostgreSQL do.
const mysqlTableOptions = " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin"

// mysqlIndexableText is the type of a text column MySQL has to index: it
// cannot index unbounded text, so a primary key, unique or indexed bare
// string is VARCHAR(255), the bound db.Store enforces for it as well.
const mysqlIndexableText = "VARCHAR(255)"

// mysqlUnboundedText reports whether a migration column type is unbounded
// text (the type tokens Diff writes, with anything unknown stored as text).
func mysqlUnboundedText(columnType string) bool {
	switch columnType {
	case "varchar", "boolean", "real", "timestamp", "integer":
		return false
	}
	return true
}

// mysqlForeignKeyName names the constraint for a foreign key column, within
// MySQL's 64-character limit.
func mysqlForeignKeyName(table, column string) string {
	name := "fk_" + table + "_" + column
	if len(name) <= 64 {
		return name
	}
	return fmt.Sprintf("%s_%08x", name[:55], crc32.ChecksumIEEE([]byte(name)))
}

// mysqlForeignKeySQL is the table-level clause that makes a foreign key real:
// InnoDB parses a column-level REFERENCES and ignores it. The action is the
// default RESTRICT, like the other dialects.
func mysqlForeignKeySQL(table, column, references, referencedColumn string) string {
	return fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		quote(db.MySQL, mysqlForeignKeyName(table, column)), quote(db.MySQL, column),
		quote(db.MySQL, references), quote(db.MySQL, referencedColumn))
}

// mysqlCreateTable creates the table with its foreign keys, then its indexes.
func mysqlCreateTable(ctx context.Context, sqlDB *sql.DB, s CreateTable) error {
	var foreignKeys []string
	for _, c := range s.Columns {
		if c.References == "" {
			continue
		}
		referenced, err := mysqlReferencedColumn(ctx, sqlDB, s, c.References)
		if err != nil {
			return err
		}
		foreignKeys = append(foreignKeys, mysqlForeignKeySQL(s.Table, c.Name, c.References, referenced))
	}
	return execAll(ctx, sqlDB, createTableWith(db.MySQL, s, foreignKeys))
}

// mysqlReferencedColumn finds the column a foreign key points at: the
// referenced table's primary key. A table referencing itself has no row in
// the database yet, so its own definition answers.
func mysqlReferencedColumn(ctx context.Context, sqlDB *sql.DB, s CreateTable, references string) (string, error) {
	if references == s.Table {
		for _, c := range s.Columns {
			if c.PrimaryKey {
				return c.Name, nil
			}
		}
		return "", fmt.Errorf("tango migration: %s references itself but has no primary key", s.Table)
	}
	return mysqlPrimaryKey(ctx, sqlDB, references)
}

// mysqlPrimaryKey is the table's single primary-key column.
func mysqlPrimaryKey(ctx context.Context, sqlDB *sql.DB, table string) (string, error) {
	rows, err := sqlDB.QueryContext(ctx, `SELECT column_name FROM information_schema.key_column_usage
		WHERE table_schema = DATABASE() AND table_name = ? AND constraint_name = 'PRIMARY' ORDER BY ordinal_position`, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(names) != 1 {
		return "", fmt.Errorf("tango migration: table %q needs exactly one primary-key column for a foreign key to reference, found %d", table, len(names))
	}
	return names[0], nil
}

// mysqlAddColumn adds the column and, for a foreign key, its constraint in
// the same statement.
func mysqlAddColumn(ctx context.Context, sqlDB *sql.DB, s AddColumn) error {
	statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", quote(db.MySQL, s.Table), addColumnDefSQL(db.MySQL, s.Column))
	if s.Column.References != "" {
		referenced, err := mysqlPrimaryKey(ctx, sqlDB, s.Column.References)
		if err != nil {
			return err
		}
		statement += ", ADD " + mysqlForeignKeySQL(s.Table, s.Column.Name, s.Column.References, referenced)
	}
	return exec(ctx, sqlDB, statement)
}

// mysqlDropColumn drops a column's foreign keys first: MySQL refuses to drop
// a column a constraint needs.
func mysqlDropColumn(ctx context.Context, sqlDB *sql.DB, s DropColumn) error {
	rows, err := sqlDB.QueryContext(ctx, `SELECT DISTINCT constraint_name FROM information_schema.key_column_usage
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ? AND referenced_table_name IS NOT NULL`, s.Table, s.Column)
	if err != nil {
		return err
	}
	var constraints []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		constraints = append(constraints, name)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, name := range constraints {
		if err := exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", quote(db.MySQL, s.Table), quote(db.MySQL, name))); err != nil {
			return err
		}
	}
	return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", quote(db.MySQL, s.Table), quote(db.MySQL, s.Column)))
}

// columnInfo is what MySQL reports about one column.
type mysqlColumnInfo struct {
	dataType string
	nullable bool
}

func mysqlColumn(ctx context.Context, sqlDB queryRower, table, column string) (mysqlColumnInfo, error) {
	var info mysqlColumnInfo
	var nullable string
	err := sqlDB.QueryRowContext(ctx, `SELECT data_type, is_nullable FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&info.dataType, &nullable)
	if errors.Is(err, sql.ErrNoRows) {
		return info, fmt.Errorf("tango migration: column %q does not exist on table %q", column, table)
	}
	info.nullable = nullable == "YES"
	return info, err
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ensureIndexable makes a column MySQL can index. A text column created
// before it was unique or indexed is LONGTEXT, which MySQL cannot index, so it
// becomes VARCHAR(255) first (a value longer than that fails the change
// instead of being cut). Other dialects, and columns that are already
// indexable, are left alone.
func ensureIndexable(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table, column string) error {
	if dialect != db.MySQL {
		return nil
	}
	info, err := mysqlColumn(ctx, sqlDB, table, column)
	if err != nil {
		return err
	}
	switch info.dataType {
	case "text", "tinytext", "mediumtext", "longtext":
		return exec(ctx, sqlDB, mysqlModifySQL(table, column, mysqlIndexableText, info.nullable, ""))
	}
	return nil
}

func mysqlModifySQL(table, column, columnType string, nullable bool, defaultLiteral string) string {
	statement := fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s", quote(db.MySQL, table), quote(db.MySQL, column), columnType)
	if !nullable {
		statement += " NOT NULL"
	}
	if defaultLiteral != "" {
		statement += " DEFAULT " + defaultLiteral
	}
	return statement
}

// mysqlColumnIndexed reports whether any index covers the column.
func mysqlColumnIndexed(ctx context.Context, sqlDB *sql.DB, table, column string) (bool, error) {
	var n int
	err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&n)
	return n > 0, err
}

// mysqlRenameIndex renames the index if the table has it.
func mysqlRenameIndex(ctx context.Context, tx *sql.Tx, table, from, to string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, table, from).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME INDEX %s TO %s", quote(db.MySQL, table), quote(db.MySQL, from), quote(db.MySQL, to)))
	return err
}

// mysqlAlterColumnType changes a column's type with MODIFY COLUMN, keeping
// its NOT NULL. MySQL converts the values itself, except a boolean becoming
// text, which would read "1" and "0" where the other dialects read "true" and
// "false": that goes through a short string column and an explicit rewrite.
func mysqlAlterColumnType(ctx context.Context, sqlDB *sql.DB, s AlterColumnType) error {
	info, err := mysqlColumn(ctx, sqlDB, s.Table, s.Column)
	if err != nil {
		return err
	}
	target := baseTypeSQL(db.MySQL, s.To, s.ToLength)
	if mysqlUnboundedText(s.To) {
		indexed, err := mysqlColumnIndexed(ctx, sqlDB, s.Table, s.Column)
		if err != nil {
			return err
		}
		if indexed {
			target = mysqlIndexableText
		}
	}
	var reals []realValue
	if s.From == "real" && s.To == "text" {
		// MySQL writes a DOUBLE as text its own way ("1e20", "0.0000001");
		// the values are read first and rewritten in the form every dialect
		// agrees on (canonicalRealText) once the column is text.
		if reals, err = readRealValues(ctx, sqlDB, s.Table, s.Column); err != nil {
			return err
		}
	}
	if s.From == "boolean" && s.To == "text" {
		if err := exec(ctx, sqlDB, mysqlModifySQL(s.Table, s.Column, "VARCHAR(5)", info.nullable, "")); err != nil {
			return err
		}
		column := quote(db.MySQL, s.Column)
		if err := exec(ctx, sqlDB, fmt.Sprintf("UPDATE %s SET %s = CASE WHEN %s = '1' THEN 'true' WHEN %s = '0' THEN 'false' END",
			quote(db.MySQL, s.Table), column, column, column)); err != nil {
			return err
		}
	}
	defaultLiteral := s.Default
	if defaultLiteral != "" {
		defaultLiteral = defaultLiteralSQL(db.MySQL, target, defaultLiteral)
	}
	if err := exec(ctx, sqlDB, mysqlModifySQL(s.Table, s.Column, target, info.nullable, defaultLiteral)); err != nil {
		return err
	}
	return writeRealValues(ctx, sqlDB, s.Table, s.Column, reals)
}

// realValue is one row's real value, found by its primary key.
type realValue struct {
	key   any
	value float64
}

func readRealValues(ctx context.Context, sqlDB *sql.DB, table, column string) ([]realValue, error) {
	pk, err := mysqlPrimaryKey(ctx, sqlDB, table)
	if err != nil {
		return nil, fmt.Errorf("tango migration: %s.%s: changing real to text needs a primary key: %w", table, column, err)
	}
	rows, err := sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IS NOT NULL",
		quote(db.MySQL, pk), quote(db.MySQL, column), quote(db.MySQL, table), quote(db.MySQL, column)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []realValue
	for rows.Next() {
		var v realValue
		if err := rows.Scan(&v.key, &v.value); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func writeRealValues(ctx context.Context, sqlDB *sql.DB, table, column string, values []realValue) error {
	if len(values) == 0 {
		return nil
	}
	pk, err := mysqlPrimaryKey(ctx, sqlDB, table)
	if err != nil {
		return err
	}
	update := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", quote(db.MySQL, table), quote(db.MySQL, column), quote(db.MySQL, pk))
	for _, v := range values {
		if _, err := sqlDB.ExecContext(ctx, update, canonicalRealText(v.value), v.key); err != nil {
			return err
		}
	}
	return nil
}

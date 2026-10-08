package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/sqlident"
)

// sqliteColumn is one column of a SQLite table as a rebuild recreates it:
// everything PRAGMA table_info and foreign_key_list report, plus source, the
// expression its value is copied from (the old column, quoted, unless a
// reshape changes it).
type sqliteColumn struct {
	name          string
	declType      string
	notNull       bool
	defaultValue  sql.NullString
	primaryKey    bool
	autoIncrement bool
	references    string
	source        string
}

func (c sqliteColumn) definition() string {
	def := quote(db.SQLite, c.name) + " " + c.declType
	if c.primaryKey {
		def += " PRIMARY KEY"
		if c.autoIncrement {
			def += " AUTOINCREMENT"
		}
	}
	if c.notNull {
		def += " NOT NULL"
	}
	if c.defaultValue.Valid {
		def += " DEFAULT " + c.defaultValue.String
	}
	if c.references != "" {
		def += " REFERENCES " + quote(db.SQLite, c.references)
	}
	return def
}

// sqliteIndex is an index created on the table by CREATE INDEX, recreated
// from its own SQL after the rebuild unless it covers a removed column.
type sqliteIndex struct {
	sql     string
	columns []string
}

// rebuildSQLiteTable changes table's columns the way SQLite's own
// documentation prescribes for changes ALTER TABLE cannot make: on one
// connection with foreign key enforcement off, and inside a transaction,
// it creates a new table with the reshaped columns, copies every row
// across, drops the old table, renames the new one into place, recreates
// the old table's indexes, keeps its AUTOINCREMENT counter, and checks
// every foreign key before committing. Each column keeps its type,
// NOT NULL, DEFAULT, primary key and REFERENCES unless reshape changes
// them, and other tables' references to table keep pointing at it. If any
// statement fails, the transaction rolls back and table is left as it was.
func rebuildSQLiteTable(ctx context.Context, sqlDB *sql.DB, table string, reshape func([]sqliteColumn) ([]sqliteColumn, error), afterCopy copyFixup) (err error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var foreignKeys int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return err
	}
	// Enforcement must be off while the old table is dropped: with it on,
	// SQLite deletes the old table's rows first and rejects the drop if
	// other tables reference them. It cannot change inside a transaction.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() {
		if _, restoreErr := conn.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("PRAGMA foreign_keys = %d", foreignKeys)); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}()

	return inTx(ctx, conn, func(tx *sql.Tx) error {
		return rebuildInTx(ctx, tx, table, reshape, afterCopy)
	})
}

// copyFixup runs after a rebuild has copied the rows into newTable, before
// the old table is dropped, to rewrite values SQL alone cannot convert
// exactly. It may be nil.
type copyFixup func(ctx context.Context, tx *sql.Tx, oldTable, newTable string, columns []sqliteColumn) error

func rebuildInTx(ctx context.Context, tx *sql.Tx, table string, reshape func([]sqliteColumn) ([]sqliteColumn, error), afterCopy copyFixup) error {
	columns, err := readSQLiteColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	indexes, err := readSQLiteIndexes(ctx, tx, table)
	if err != nil {
		return err
	}
	sequence, hasSequence, err := readSQLiteSequence(ctx, tx, table)
	if err != nil {
		return err
	}

	reshaped, err := reshape(columns)
	if err != nil {
		return err
	}

	tempTable := table + "_tango_rebuild"
	defs := make([]string, len(reshaped))
	names := make([]string, len(reshaped))
	sources := make([]string, len(reshaped))
	for i, c := range reshaped {
		defs[i] = c.definition()
		names[i] = c.name
		sources[i] = c.source
	}
	for _, statement := range []string{
		fmt.Sprintf("CREATE TABLE %s (%s)", quote(db.SQLite, tempTable), strings.Join(defs, ", ")),
		fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", quote(db.SQLite, tempTable), sqlident.QuoteAll(identStyle(db.SQLite), names), strings.Join(sources, ", "), quote(db.SQLite, table)),
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if afterCopy != nil {
		if err := afterCopy(ctx, tx, table, tempTable, reshaped); err != nil {
			return err
		}
	}
	statements := []string{
		fmt.Sprintf("DROP TABLE %s", quote(db.SQLite, table)),
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quote(db.SQLite, tempTable), quote(db.SQLite, table)),
	}
	for _, index := range indexes {
		if coversOnly(index.columns, names) {
			statements = append(statements, index.sql)
		}
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	if hasSequence {
		if _, err := tx.ExecContext(ctx, "UPDATE sqlite_sequence SET seq = MAX(seq, $1) WHERE name = $2", sequence, table); err != nil {
			return err
		}
	}
	return checkSQLiteForeignKeys(ctx, tx)
}

func coversOnly(indexColumns, tableColumns []string) bool {
	for _, c := range indexColumns {
		if !slices.Contains(tableColumns, c) {
			return false
		}
	}
	return true
}

func readSQLiteColumns(ctx context.Context, tx *sql.Tx, table string) ([]sqliteColumn, error) {
	var createSQL string
	if err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = $1", table).Scan(&createSQL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("tango migration: table %q does not exist", table)
		}
		return nil, err
	}
	autoIncrement := strings.Contains(strings.ToUpper(createSQL), "AUTOINCREMENT")

	references, err := readSQLiteReferences(ctx, tx, table)
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quote(db.SQLite, table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []sqliteColumn
	for rows.Next() {
		var (
			cid, notNull, pk int
			c                sqliteColumn
		)
		if err := rows.Scan(&cid, &c.name, &c.declType, &notNull, &c.defaultValue, &pk); err != nil {
			return nil, err
		}
		c.notNull = notNull != 0
		c.primaryKey = pk != 0
		c.autoIncrement = c.primaryKey && autoIncrement
		c.references = references[c.name]
		c.source = quote(db.SQLite, c.name)
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

// readSQLiteReferences maps each of table's foreign key columns to the
// table it references.
func readSQLiteReferences(ctx context.Context, tx *sql.Tx, table string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", quote(db.SQLite, table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	references := make(map[string]string)
	for rows.Next() {
		var (
			id, seq                   int
			target, from              string
			to                        sql.NullString
			onUpdate, onDelete, match string
		)
		if err := rows.Scan(&id, &seq, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		references[from] = target
	}
	return references, rows.Err()
}

// sqliteCreatedIndexesSQL lists the indexes created on a table by
// CREATE INDEX, not the ones SQLite makes for a constraint itself.
const sqliteCreatedIndexesSQL = "SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = $1 AND sql IS NOT NULL"

func readSQLiteIndexes(ctx context.Context, tx *sql.Tx, table string) ([]sqliteIndex, error) {
	rows, err := tx.QueryContext(ctx, sqliteCreatedIndexesSQL, table)
	if err != nil {
		return nil, err
	}
	type named struct{ name, sql string }
	var found []named
	for rows.Next() {
		var n named
		if err := rows.Scan(&n.name, &n.sql); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, n)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	indexes := make([]sqliteIndex, len(found))
	for i, n := range found {
		columns, err := sqliteIndexColumns(ctx, tx, n.name)
		if err != nil {
			return nil, err
		}
		indexes[i] = sqliteIndex{sql: n.sql, columns: columns}
	}
	return indexes, nil
}

func sqliteIndexColumns(ctx context.Context, tx *sql.Tx, index string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info(%s)", quote(db.SQLite, index)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var (
			seqno, cid int
			name       sql.NullString
		)
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		columns = append(columns, name.String)
	}
	return columns, rows.Err()
}

// readSQLiteSequence returns table's AUTOINCREMENT counter, so the rebuilt
// table never hands out an id the old one already used.
func readSQLiteSequence(ctx context.Context, tx *sql.Tx, table string) (int64, bool, error) {
	var hasSequenceTable int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'").Scan(&hasSequenceTable); err != nil {
		return 0, false, err
	}
	if hasSequenceTable == 0 {
		return 0, false, nil
	}
	var sequence int64
	err := tx.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name = $1", table).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return sequence, err == nil, err
}

// checkSQLiteForeignKeys fails if any row references a row that does not
// exist, which the rebuild could only cause by losing rows.
func checkSQLiteForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var (
			table, parent string
			rowid         sql.NullInt64
			fkid          int
		)
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		return fmt.Errorf("tango migration: rebuild left %q row %d referencing a missing %q row", table, rowid.Int64, parent)
	}
	return rows.Err()
}

// dropSQLiteColumn returns a reshape that removes column, failing if table
// has no such column.
func dropSQLiteColumn(table, column string) func([]sqliteColumn) ([]sqliteColumn, error) {
	return func(columns []sqliteColumn) ([]sqliteColumn, error) {
		kept := slices.DeleteFunc(slices.Clone(columns), func(c sqliteColumn) bool { return c.name == column })
		if len(kept) == len(columns) {
			return nil, fmt.Errorf("tango migration: column %q does not exist on table %q", column, table)
		}
		return kept, nil
	}
}

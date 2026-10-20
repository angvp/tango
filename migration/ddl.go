package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/sqlident"
)

// ApplyStep executes step against sqlDB, translating it into the DDL
// appropriate for dialect. A SQLite step it cannot express as a direct
// ALTER TABLE (DropColumn, AlterColumnType) is applied by rebuilding the
// table, keeping everything else about it (see rebuildSQLiteTable).
// It is exported so ApplyPending and the tango CLI can share DDL translation;
// application code should use tango migrate rather than calling ApplyStep
// directly.
func ApplyStep(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, step Step) error {
	if err := checkDialect(dialect); err != nil {
		return err
	}
	return applyStep(ctx, sqlDB, dialect, step, false)
}

// applyStep is ApplyStep. dataChecked says the caller already ran
// preflightMigration for the whole migration, so a narrowing is not scanned
// a second time; a direct ApplyStep always passes false and keeps its guard.
func applyStep(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, step Step, dataChecked bool) error {
	switch s := step.(type) {
	case CreateTable:
		for _, c := range s.Columns {
			if err := validateColumn(s.Table, c); err != nil {
				return err
			}
		}
		if dialect == db.MySQL {
			return mysqlCreateTable(ctx, sqlDB, s)
		}
		return execAll(ctx, sqlDB, createTableSQL(dialect, s))
	case DropTable:
		return exec(ctx, sqlDB, fmt.Sprintf("DROP TABLE %s", quote(dialect, s.Table)))
	case AddColumn:
		if err := validateColumn(s.Table, s.Column); err != nil {
			return err
		}
		if dialect == db.MySQL {
			return mysqlAddColumn(ctx, sqlDB, s)
		}
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", quote(dialect, s.Table), addColumnDefSQL(dialect, s.Column)))
	case DropColumn:
		switch dialect {
		case db.MySQL:
			return mysqlDropColumn(ctx, sqlDB, s)
		case db.Postgres:
			return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", quote(dialect, s.Table), quote(dialect, s.Column)))
		case db.SQLite:
			return rebuildSQLiteTable(ctx, sqlDB, s.Table, dropSQLiteColumn(s.Table, s.Column), nil)
		}
		return unsupportedDialect(dialect)
	case AlterColumnUnique:
		return applyUniqueIndex(ctx, sqlDB, dialect, s.Table, s.Column, s.Unique)
	case CreateIndex:
		if err := ensureIndexable(ctx, sqlDB, dialect, s.Table, s.Column); err != nil {
			return err
		}
		return exec(ctx, sqlDB, createIndexSQL(dialect, false, indexName(s.Table, s.Column), s.Table, s.Column))
	case DropIndex:
		return exec(ctx, sqlDB, dropIndexSQL(dialect, indexName(s.Table, s.Column), s.Table))
	case RenameColumn:
		return renameColumn(ctx, sqlDB, dialect, s)
	case AlterColumnType:
		return alterColumnType(ctx, sqlDB, dialect, s, dataChecked)
	case RenameTable:
		return renameTable(ctx, sqlDB, dialect, s)
	default:
		return fmt.Errorf("tango migration: unsupported step type %T", step)
	}
}

func exec(ctx context.Context, sqlDB *sql.DB, query string) error {
	_, err := sqlDB.ExecContext(ctx, query)
	return err
}

func execAll(ctx context.Context, sqlDB *sql.DB, queries []string) error {
	for _, q := range queries {
		if err := exec(ctx, sqlDB, q); err != nil {
			return err
		}
	}
	return nil
}

// quote quotes a table, column or index name for dialect. Every identifier
// in the DDL ApplyStep generates goes through it, so a reserved word such as
// "user" or "order" is a valid name. Users' own raw SQL is not quoted for
// them. The tango_migrations tracking table's SQL is a fixed literal, not
// generated from names, and none of its names are reserved, so it is left
// as is (AppliedMigrations has no dialect to quote with).
func quote(dialect db.Dialect, name string) string {
	return sqlident.Quote(identStyle(dialect), name)
}

func identStyle(dialect db.Dialect) sqlident.Style {
	return pick(dialect, sqlident.Backtick, sqlident.DoubleQuote, sqlident.Backtick)
}

func indexName(table, column string) string {
	return fmt.Sprintf("idx_%s_%s", table, column)
}

func uniqueIndexName(table, column string) string {
	return "uniq_" + table + "_" + column
}

// createIndexSQL returns "CREATE [UNIQUE] INDEX name ON table (column)"
// with every identifier quoted.
func createIndexSQL(dialect db.Dialect, unique bool, name, table, column string) string {
	verb := "CREATE INDEX"
	if unique {
		verb = "CREATE UNIQUE INDEX"
	}
	return fmt.Sprintf("%s %s ON %s (%s)", verb, quote(dialect, name), quote(dialect, table), quote(dialect, column))
}

// applyUniqueIndex enforces (or lifts) uniqueness via a unique index rather
// than a table constraint, so it applies identically and directly on both
// SQLite and Postgres with no table-rebuild required.
func applyUniqueIndex(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table, column string, unique bool) error {
	name := uniqueIndexName(table, column)
	if unique {
		if err := ensureIndexable(ctx, sqlDB, dialect, table, column); err != nil {
			return err
		}
		return exec(ctx, sqlDB, createIndexSQL(dialect, true, name, table, column))
	}
	return exec(ctx, sqlDB, dropIndexSQL(dialect, name, table))
}

// dropIndexSQL drops the named index. MySQL scopes an index to its table, so
// it needs ON table.
func dropIndexSQL(dialect db.Dialect, name, table string) string {
	drop := fmt.Sprintf("DROP INDEX %s", quote(dialect, name))
	if dialect == db.MySQL {
		drop += " ON " + quote(dialect, table)
	}
	return drop
}

func createTableSQL(dialect db.Dialect, s CreateTable) []string {
	return createTableWith(dialect, s, nil)
}

// createTableWith is createTableSQL with extra table-level definitions (MySQL's
// FOREIGN KEY clauses) after the columns.
func createTableWith(dialect db.Dialect, s CreateTable, extraDefs []string) []string {
	defs := make([]string, len(s.Columns), len(s.Columns)+len(extraDefs))
	var uniqueIndexes []string
	for i, c := range s.Columns {
		defs[i] = columnDefSQL(dialect, c)
		if c.Unique {
			uniqueIndexes = append(uniqueIndexes, createIndexSQL(dialect, true, uniqueIndexName(s.Table, c.Name), s.Table, c.Name))
		}
		if c.Indexed {
			uniqueIndexes = append(uniqueIndexes, createIndexSQL(dialect, false, indexName(s.Table, c.Name), s.Table, c.Name))
		}
	}

	defs = append(defs, extraDefs...)
	statements := []string{fmt.Sprintf("CREATE TABLE %s (%s)%s", quote(dialect, s.Table), strings.Join(defs, ", "), pick(dialect, "", "", mysqlTableOptions))}
	return append(statements, uniqueIndexes...)
}

// columnDefSQL generates a column definition for CreateTable. It
// deliberately ignores Column.Default: Default exists solely so AddColumn
// (see addColumnDefSQL) can backfill an existing table's pre-existing rows
// — a table being freshly created has no rows to backfill, and its columns
// already get whatever value the application writes on insert, so Default
// has nothing to do here. Keeping CreateTable Default-blind is what keeps
// Default a narrow AddColumn-only escape hatch rather than a general
// default-value system.
func columnDefSQL(dialect db.Dialect, c Column) string {
	name := quote(dialect, c.Name)
	if c.PrimaryKey && c.Type == "integer" {
		return name + " " + pick(dialect, "INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY", "BIGINT AUTO_INCREMENT PRIMARY KEY") + referencesSQL(dialect, c)
	}

	typ := columnTypeSQL(dialect, c)
	if c.PrimaryKey {
		typ += " PRIMARY KEY"
	}
	return name + " " + typ + referencesSQL(dialect, c)
}

// addColumnDefSQL generates a column definition for AddColumn, honoring
// Column.Default (a raw SQL literal, e.g. "TRUE") as a trailing
// NOT NULL DEFAULT clause when set, so pre-existing rows in the table being
// altered backfill to that value instead of going NULL. See columnDefSQL's
// doc comment for why CreateTable does not get the same treatment.
func addColumnDefSQL(dialect db.Dialect, c Column) string {
	def := columnDefSQL(dialect, c)
	if c.Default == "" {
		return def
	}
	return def + " NOT NULL DEFAULT " + defaultLiteralSQL(dialect, columnTypeSQL(dialect, c), c.Default)
}

// defaultLiteralSQL is a column default as the dialect accepts it for a column
// of columnType. MySQL gives a TEXT column a default only as an expression,
// which needs its own parentheses.
func defaultLiteralSQL(dialect db.Dialect, columnType, literal string) string {
	if dialect == db.MySQL && strings.HasSuffix(columnType, "TEXT") {
		return "(" + literal + ")"
	}
	return literal
}

// referencesSQL returns the inline "REFERENCES table" clause for a foreign
// key column, or "" for an ordinary column. The clause carries no ON DELETE
// action, so it defaults to RESTRICT/NO ACTION on both dialects — cascade
// delete is implemented in application code by Store.Delete, never by the
// database.
func referencesSQL(dialect db.Dialect, c Column) string {
	if c.References == "" || dialect == db.MySQL {
		// MySQL parses an inline REFERENCES and ignores it, so its foreign
		// keys are separate FOREIGN KEY clauses (see mysqlForeignKeySQL).
		return ""
	}
	return " REFERENCES " + quote(dialect, c.References)
}

// columnTypeSQL is the type of c as a column definition. It is baseTypeSQL,
// except that MySQL cannot index unbounded text: a text column that is a
// primary key, unique or indexed is VARCHAR(255) there.
func columnTypeSQL(dialect db.Dialect, c Column) string {
	if dialect == db.MySQL && mysqlUnboundedText(c.Type) && (c.PrimaryKey || c.Unique || c.Indexed) {
		return mysqlIndexableText
	}
	return baseTypeSQL(dialect, c.Type, c.Length)
}

func baseTypeSQL(dialect db.Dialect, columnType string, length int) string {
	switch columnType {
	case "text":
		return pick(dialect, "TEXT", "TEXT", "LONGTEXT")
	case "varchar":
		return fmt.Sprintf("VARCHAR(%d)", length)
	case "boolean":
		return "BOOLEAN"
	case "real":
		return pick(dialect, "REAL", "DOUBLE PRECISION", "DOUBLE")
	case "timestamp":
		return pick(dialect, "TIMESTAMP", "TIMESTAMPTZ", "DATETIME(6)")
	case "integer":
		return pick(dialect, "INTEGER", "BIGINT", "BIGINT")
	default:
		return pick(dialect, "TEXT", "TEXT", "LONGTEXT")
	}
}

// checkDialect refuses a Dialect this package has no SQL for, so a value from
// a newer release can never be treated as SQLite.
func checkDialect(dialect db.Dialect) error {
	switch dialect {
	case db.SQLite, db.Postgres, db.MySQL:
		return nil
	}
	return unsupportedDialect(dialect)
}

func unsupportedDialect(dialect db.Dialect) error {
	return fmt.Errorf("tango migration: unsupported dialect %d", int(dialect))
}

// pick returns the value for dialect. It takes one argument per supported
// dialect, so adding a dialect makes every call site fail to compile until
// it says what the new dialect does; an unknown value panics rather than
// quietly taking another dialect's branch. Both arguments are evaluated.
func pick[T any](dialect db.Dialect, sqlite, postgres, mysql T) T {
	switch dialect {
	case db.SQLite:
		return sqlite
	case db.Postgres:
		return postgres
	case db.MySQL:
		return mysql
	}
	panic(unsupportedDialect(dialect).Error())
}

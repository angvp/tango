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
// ALTER TABLE (DropColumn) is applied by rebuilding the table, keeping
// everything else about it (see rebuildSQLiteTable).
// It is exported so ApplyPending and the tango CLI can share DDL translation;
// application code should use tango migrate rather than calling ApplyStep
// directly.
func ApplyStep(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, step Step) error {
	switch s := step.(type) {
	case CreateTable:
		return execAll(ctx, sqlDB, createTableSQL(dialect, s))
	case DropTable:
		return exec(ctx, sqlDB, fmt.Sprintf("DROP TABLE %s", quote(dialect, s.Table)))
	case AddColumn:
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", quote(dialect, s.Table), addColumnDefSQL(dialect, s.Column)))
	case DropColumn:
		if dialect == db.Postgres {
			return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", quote(dialect, s.Table), quote(dialect, s.Column)))
		}
		return rebuildSQLiteTable(ctx, sqlDB, s.Table, dropSQLiteColumn(s.Table, s.Column))
	case AlterColumnUnique:
		return applyUniqueIndex(ctx, sqlDB, dialect, s.Table, s.Column, s.Unique)
	case CreateIndex:
		return exec(ctx, sqlDB, createIndexSQL(dialect, false, indexName(s.Table, s.Column), s.Table, s.Column))
	case DropIndex:
		return exec(ctx, sqlDB, fmt.Sprintf("DROP INDEX %s", quote(dialect, indexName(s.Table, s.Column))))
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
	if dialect == db.Postgres {
		return sqlident.DoubleQuote
	}
	return sqlident.Backtick
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
		return exec(ctx, sqlDB, createIndexSQL(dialect, true, name, table, column))
	}
	return exec(ctx, sqlDB, fmt.Sprintf("DROP INDEX %s", quote(dialect, name)))
}

func createTableSQL(dialect db.Dialect, s CreateTable) []string {
	defs := make([]string, len(s.Columns))
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

	statements := []string{fmt.Sprintf("CREATE TABLE %s (%s)", quote(dialect, s.Table), strings.Join(defs, ", "))}
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
		if dialect == db.Postgres {
			return name + " BIGSERIAL PRIMARY KEY" + referencesSQL(dialect, c)
		}
		return name + " INTEGER PRIMARY KEY AUTOINCREMENT" + referencesSQL(dialect, c)
	}

	typ := baseTypeSQL(dialect, c.Type)
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
	return def + " NOT NULL DEFAULT " + c.Default
}

// referencesSQL returns the inline "REFERENCES table" clause for a foreign
// key column, or "" for an ordinary column. The clause carries no ON DELETE
// action, so it defaults to RESTRICT/NO ACTION on both dialects — cascade
// delete is implemented in application code by Store.Delete, never by the
// database.
func referencesSQL(dialect db.Dialect, c Column) string {
	if c.References == "" {
		return ""
	}
	return " REFERENCES " + quote(dialect, c.References)
}

func baseTypeSQL(dialect db.Dialect, columnType string) string {
	switch columnType {
	case "text":
		return "TEXT"
	case "boolean":
		return "BOOLEAN"
	case "real":
		if dialect == db.Postgres {
			return "DOUBLE PRECISION"
		}
		return "REAL"
	case "timestamp":
		if dialect == db.Postgres {
			return "TIMESTAMPTZ"
		}
		return "TIMESTAMP"
	case "integer":
		if dialect == db.Postgres {
			return "BIGINT"
		}
		return "INTEGER"
	default:
		return "TEXT"
	}
}

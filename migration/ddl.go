package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
)

// ApplyStep executes step against sqlDB, translating it into the DDL
// appropriate for dialect. SQLite steps it cannot express via a direct
// ALTER TABLE (DropColumn, AlterColumnUnique) are applied via the standard
// table-rebuild pattern: create a new table with the desired shape, copy
// data across, drop the old table, and rename the new one into place.
// It is exported so ApplyPending and the tango CLI can share DDL translation;
// application code should use tango migrate rather than calling ApplyStep
// directly.
func ApplyStep(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, step Step) error {
	switch s := step.(type) {
	case CreateTable:
		return execAll(ctx, sqlDB, createTableSQL(dialect, s))
	case DropTable:
		return exec(ctx, sqlDB, fmt.Sprintf("DROP TABLE %s", s.Table))
	case AddColumn:
		return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", s.Table, addColumnDefSQL(dialect, s.Column)))
	case DropColumn:
		if dialect == db.Postgres {
			return exec(ctx, sqlDB, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", s.Table, s.Column))
		}
		return rebuildTableDroppingColumn(ctx, sqlDB, s.Table, s.Column)
	case AlterColumnUnique:
		return applyUniqueIndex(ctx, sqlDB, s.Table, s.Column, s.Unique)
	case CreateIndex:
		return exec(ctx, sqlDB, fmt.Sprintf("CREATE INDEX %s ON %s (%s)", indexName(s.Table, s.Column), s.Table, s.Column))
	case DropIndex:
		return exec(ctx, sqlDB, fmt.Sprintf("DROP INDEX %s", indexName(s.Table, s.Column)))
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

func indexName(table, column string) string {
	return fmt.Sprintf("idx_%s_%s", table, column)
}

// applyUniqueIndex enforces (or lifts) uniqueness via a unique index rather
// than a table constraint, so it applies identically and directly on both
// SQLite and Postgres with no table-rebuild required.
func applyUniqueIndex(ctx context.Context, sqlDB *sql.DB, table, column string, unique bool) error {
	name := "uniq_" + table + "_" + column
	if unique {
		return exec(ctx, sqlDB, fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", name, table, column))
	}
	return exec(ctx, sqlDB, fmt.Sprintf("DROP INDEX %s", name))
}

func createTableSQL(dialect db.Dialect, s CreateTable) []string {
	defs := make([]string, len(s.Columns))
	var uniqueIndexes []string
	for i, c := range s.Columns {
		defs[i] = columnDefSQL(dialect, c)
		if c.Unique {
			uniqueIndexes = append(uniqueIndexes, fmt.Sprintf(
				"CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s)", s.Table, c.Name, s.Table, c.Name,
			))
		}
		if c.Indexed {
			uniqueIndexes = append(uniqueIndexes, fmt.Sprintf(
				"CREATE INDEX %s ON %s (%s)", indexName(s.Table, c.Name), s.Table, c.Name,
			))
		}
	}

	statements := []string{fmt.Sprintf("CREATE TABLE %s (%s)", s.Table, strings.Join(defs, ", "))}
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
	if c.PrimaryKey && c.Type == "integer" {
		if dialect == db.Postgres {
			return c.Name + " BIGSERIAL PRIMARY KEY" + referencesSQL(c)
		}
		return c.Name + " INTEGER PRIMARY KEY AUTOINCREMENT" + referencesSQL(c)
	}

	typ := baseTypeSQL(dialect, c.Type)
	if c.PrimaryKey {
		typ += " PRIMARY KEY"
	}
	return c.Name + " " + typ + referencesSQL(c)
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
func referencesSQL(c Column) string {
	if c.References == "" {
		return ""
	}
	return " REFERENCES " + c.References
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

// rebuildTableDroppingColumn implements SQLite's standard table-rebuild
// pattern for a DropColumn step: create a new table with the desired shape,
// copy data across, drop the old table, and rename the new one into place.
func rebuildTableDroppingColumn(ctx context.Context, sqlDB *sql.DB, table, dropColumn string) error {
	columns, err := sqliteTableColumns(ctx, sqlDB, table)
	if err != nil {
		return err
	}

	var remaining []sqliteColumnInfo
	for _, c := range columns {
		if c.name != dropColumn {
			remaining = append(remaining, c)
		}
	}
	if len(remaining) == len(columns) {
		return fmt.Errorf("tango migration: column %q does not exist on table %q", dropColumn, table)
	}

	tempTable := table + "_tango_rebuild"

	defs := make([]string, len(remaining))
	names := make([]string, len(remaining))
	for i, c := range remaining {
		def := c.name + " " + c.declType
		if c.primaryKey {
			def += " PRIMARY KEY"
		}
		defs[i] = def
		names[i] = c.name
	}

	statements := []string{
		fmt.Sprintf("CREATE TABLE %s (%s)", tempTable, strings.Join(defs, ", ")),
		fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", tempTable, strings.Join(names, ", "), strings.Join(names, ", "), table),
		fmt.Sprintf("DROP TABLE %s", table),
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tempTable, table),
	}

	return execAll(ctx, sqlDB, statements)
}

type sqliteColumnInfo struct {
	name       string
	declType   string
	primaryKey bool
}

func sqliteTableColumns(ctx context.Context, sqlDB *sql.DB, table string) ([]sqliteColumnInfo, error) {
	rows, err := sqlDB.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []sqliteColumnInfo
	for rows.Next() {
		var (
			cid       int
			name      string
			declType  string
			notNull   int
			dfltValue any
			pk        int
		)
		if err := rows.Scan(&cid, &name, &declType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, sqliteColumnInfo{name: name, declType: declType, primaryKey: pk != 0})
	}
	return columns, rows.Err()
}

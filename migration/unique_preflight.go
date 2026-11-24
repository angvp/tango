package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
)

// preflightUnique fails, before any DDL, when a unique composite index in steps
// would be created over rows that already repeat a tuple. Rows with a NULL in
// any indexed column are never duplicates: all three databases accept any
// number of them. Tables and columns a step of steps itself creates hold no
// data yet and are skipped; earlier renames are undone to find the names the
// database holds now.
func preflightUnique(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, steps []Step) error {
	for i, step := range steps {
		s, ok := step.(CreateCompositeIndex)
		if !ok || !s.Unique {
			continue
		}
		table, columns, created := physicalTuple(steps[:i], s.Table, s.Columns)
		if created {
			continue
		}
		if err := preflightUniqueTuple(ctx, sqlDB, dialect, table, columns); err != nil {
			return err
		}
	}
	return nil
}

// physicalTuple resolves a table and its columns to the names the database
// holds before earlier ran; created reports a table or column earlier creates.
func physicalTuple(earlier []Step, table string, columns []string) (string, []string, bool) {
	physical := make([]string, len(columns))
	physicalTable := table
	for i, column := range columns {
		t, c, created := physicalNames(earlier, table, column)
		if created {
			return t, nil, true
		}
		physicalTable, physical[i] = t, c
	}
	return physicalTable, physical, false
}

func preflightUniqueTuple(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, table string, columns []string) error {
	fail := func(err error) error {
		return fmt.Errorf("tango migration: table %s: checking the data before a unique index on (%s): %w", table, strings.Join(columns, ", "), err)
	}
	quoted := make([]string, len(columns))
	notNull := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = quote(dialect, c)
		notNull[i] = quoted[i] + " IS NOT NULL"
	}
	grouped := fmt.Sprintf("FROM %s WHERE %s GROUP BY %s HAVING COUNT(*) > 1",
		quote(dialect, table), strings.Join(notNull, " AND "), strings.Join(quoted, ", "))

	var groups int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT 1 "+grouped+") d").Scan(&groups); err != nil {
		return fail(err)
	}
	if groups == 0 {
		return nil
	}

	example := ""
	if pk, err := primaryKeyColumn(ctx, sqlDB, dialect, table); err == nil {
		var key any
		query := fmt.Sprintf("SELECT MIN(%s) %s ORDER BY MIN(%s) LIMIT 1", quote(dialect, pk), grouped, quote(dialect, pk))
		if err := sqlDB.QueryRowContext(ctx, query).Scan(&key); err != nil {
			return fail(err)
		}
		example = ", for example the row with " + describeKey(pk, key)
	}
	noun := "groups of rows repeat"
	if groups == 1 {
		noun = "group of rows repeats"
	}
	return fmt.Errorf("tango migration: table %s: cannot create a unique index on (%s): %d duplicate %s the same values%s; remove or merge those rows first, then run the migration again",
		table, strings.Join(columns, ", "), groups, noun, example)
}

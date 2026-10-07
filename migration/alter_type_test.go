package migration

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// columnType reads column's declared type from the dialect's catalog, in
// lower case, to compare with baseTypeSQL.
func columnType(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table, column string) string {
	t.Helper()
	query := "SELECT type FROM pragma_table_info($1) WHERE name = $2"
	if dialect == db.Postgres {
		query = "SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2"
	}
	var typ string
	if err := sqlDB.QueryRow(query, table, column).Scan(&typ); err != nil {
		t.Fatalf("read type of %s.%s: %v", table, column, err)
	}
	return strings.ToLower(typ)
}

// readColumn returns column's values in id order, each scanned into a
// fresh value of the type of zero and dereferenced, nil for NULL.
func readColumn(t *testing.T, sqlDB *sql.DB, column string, zero any) []any {
	t.Helper()
	rows, err := sqlDB.Query(fmt.Sprintf("SELECT %s FROM sample ORDER BY id", column))
	if err != nil {
		t.Fatalf("read %s: %v", column, err)
	}
	defer rows.Close()
	var values []any
	for rows.Next() {
		dest := reflect.New(reflect.PointerTo(reflect.TypeOf(zero)))
		if err := rows.Scan(dest.Interface()); err != nil {
			t.Fatalf("scan %s: %v", column, err)
		}
		if dest.Elem().IsNil() {
			values = append(values, nil)
			continue
		}
		values = append(values, dest.Elem().Elem().Interface())
	}
	return values
}

// widening is one Widening type change with the rows it converts.
type widening struct {
	from, to string
	values   []string // SQL literals inserted into v, one row each
	want     []any    // v after the change, nil for NULL
	zero     any      // a value of v's new Go type
	// defaultFrom is d's default before the change, defaultTo the converted
	// default the step carries, wantDefault what d reads as afterwards.
	defaultFrom, defaultTo string
	wantDefault            any
}

var widenings = []widening{
	{from: "integer", to: "real", values: []string{"3", "-7", "NULL"}, want: []any{3.0, -7.0, nil}, zero: float64(0),
		defaultFrom: "7", defaultTo: "7.0", wantDefault: 7.0},
	{from: "integer", to: "text", values: []string{"42", "-1", "NULL"}, want: []any{"42", "-1", nil}, zero: "",
		defaultFrom: "7", defaultTo: "'7'", wantDefault: "7"},
	{from: "real", to: "text", values: []string{"1.0", "1.5", "-2.25", "0.1", "1e20", "123456789012345.0", "1e-7", "NULL"},
		want: []any{"1", "1.5", "-2.25", "0.1", "1e+20", "123456789012345", "1e-07", nil}, zero: "",
		defaultFrom: "2.5", defaultTo: "'2.5'", wantDefault: "2.5"},
	{from: "boolean", to: "integer", values: []string{"TRUE", "FALSE", "NULL"}, want: []any{int64(1), int64(0), nil}, zero: int64(0),
		defaultFrom: "TRUE", defaultTo: "1", wantDefault: int64(1)},
	{from: "boolean", to: "text", values: []string{"TRUE", "FALSE", "NULL"}, want: []any{"true", "false", nil}, zero: "",
		defaultFrom: "TRUE", defaultTo: "'true'", wantDefault: "true"},
}

// wideningFixture creates sample with v (nullable, one row per value) and d
// (NOT NULL DEFAULT defaultFrom, added later so existing rows backfill).
func wideningFixture(t *testing.T, w widening) (*sql.DB, db.Dialect) {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "sample", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "v", Type: w.from},
		}},
	)
	for _, value := range w.values {
		mustExec(t, sqlDB, "INSERT INTO sample (v) VALUES ("+value+")")
	}
	applySteps(t, sqlDB, dialect, AddColumn{Table: "sample", Column: Column{Name: "d", Type: w.from, Default: w.defaultFrom}})
	return sqlDB, dialect
}

func TestAlterColumnTypeConvertsEveryRowOnBothDialects(t *testing.T) {
	for _, w := range widenings {
		t.Run(w.from+"_to_"+w.to, func(t *testing.T) {
			sqlDB, dialect := wideningFixture(t, w)

			applySteps(t, sqlDB, dialect,
				AlterColumnType{Table: "sample", Column: "v", From: w.from, To: w.to},
				AlterColumnType{Table: "sample", Column: "d", From: w.from, To: w.to, Default: w.defaultTo},
			)

			wantType := strings.ToLower(baseTypeSQL(dialect, w.to))
			for _, column := range []string{"v", "d"} {
				if got := columnType(t, sqlDB, dialect, "sample", column); got != wantType {
					t.Fatalf("%s type = %q, want %q", column, got, wantType)
				}
			}
			if got := readColumn(t, sqlDB, "v", w.zero); !reflect.DeepEqual(got, w.want) {
				t.Fatalf("v = %#v, want %#v", got, w.want)
			}
			for i, got := range readColumn(t, sqlDB, "d", w.zero) {
				if !reflect.DeepEqual(got, w.wantDefault) {
					t.Fatalf("d row %d = %#v, want the converted default %#v", i, got, w.wantDefault)
				}
			}

			mustExec(t, sqlDB, "INSERT INTO sample (v) VALUES (NULL)")
			got := readColumn(t, sqlDB, "d", w.zero)
			if last := got[len(got)-1]; !reflect.DeepEqual(last, w.wantDefault) {
				t.Fatalf("new row's d = %#v, want the converted default %#v", last, w.wantDefault)
			}
			mustFail(t, sqlDB, "d is still NOT NULL", "INSERT INTO sample (v, d) VALUES (NULL, NULL)")
		})
	}
}

func TestFailedAlterColumnTypeChangesNothing(t *testing.T) {
	for _, w := range widenings {
		t.Run(w.from+"_to_"+w.to, func(t *testing.T) {
			sqlDB, dialect := wideningFixture(t, w)
			// A view selecting d is a real dependency: PostgreSQL refuses to
			// change the type of a column a view uses, and SQLite's rebuild
			// fails renaming its new table into place, after create, copy
			// and drop have run.
			mustExec(t, sqlDB, "CREATE VIEW sample_d AS SELECT d FROM sample")
			typeBefore := columnType(t, sqlDB, dialect, "sample", "d")
			var zeroBefore any = int64(0)
			switch w.from {
			case "real":
				zeroBefore = float64(0)
			case "boolean":
				zeroBefore = false
			}
			valuesBefore := readColumn(t, sqlDB, "d", zeroBefore)

			err := ApplyStep(context.Background(), sqlDB, dialect, AlterColumnType{Table: "sample", Column: "d", From: w.from, To: w.to, Default: w.defaultTo})
			if err == nil {
				t.Fatal("AlterColumnType succeeded although a view depends on the column, want an error")
			}

			if got := columnType(t, sqlDB, dialect, "sample", "d"); got != typeBefore {
				t.Fatalf("d type = %q after a failed change, want %q", got, typeBefore)
			}
			if got := readColumn(t, sqlDB, "d", zeroBefore); !reflect.DeepEqual(got, valuesBefore) {
				t.Fatalf("d = %#v after a failed change, want %#v", got, valuesBefore)
			}
			mustExec(t, sqlDB, "INSERT INTO sample (v) VALUES (NULL)")
			got := readColumn(t, sqlDB, "d", zeroBefore)
			if last := got[len(got)-1]; !reflect.DeepEqual(last, valuesBefore[0]) {
				t.Fatalf("new row's d = %#v, want the unchanged default %#v", last, valuesBefore[0])
			}
			var viewRows int
			if err := sqlDB.QueryRow("SELECT COUNT(*) FROM sample_d").Scan(&viewRows); err != nil {
				t.Fatalf("view stopped working: %v", err)
			}
		})
	}
}

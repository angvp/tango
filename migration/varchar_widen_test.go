package migration

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func varcharState(length int, def string) SchemaState {
	column := ColumnState{Name: "title", Type: "varchar", Length: length, Default: def}
	if length == 0 {
		column.Type = "text"
	}
	return SchemaState{Tables: map[string]TableState{"headline": {Name: "headline", App: "news", Columns: []ColumnState{
		{Name: "id", Type: "integer", PrimaryKey: true}, column,
	}}}}
}

func TestDiffTurnsSafeWideningsOfABoundedColumnIntoAlterColumnType(t *testing.T) {
	tests := []struct {
		name         string
		from, to     int
		def, wantDef string
	}{
		{"varchar to text", 100, 0, "", ""},
		{"varchar to text keeps its default", 100, 0, "'draft'", "'draft'"},
		{"a longer varchar", 100, 200, "", ""},
		{"a longer varchar keeps its default", 100, 200, "'draft'", "'draft'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migrations, err := Diff(headlineMeta(stringField("Title", tt.to)), varcharState(tt.from, tt.def))
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			step := AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: tt.from, To: "varchar", ToLength: tt.to, Default: tt.wantDef}
			if tt.to == 0 {
				step.To = "text"
			}
			want := []Migration{{App: "news", Up: []Step{step}}}
			if !reflect.DeepEqual(migrations, want) {
				t.Fatalf("migrations = %#v, want %#v (irreversible, no Down)", migrations, want)
			}
		})
	}
}

func declaredType(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table, column string) string {
	t.Helper()
	query := "SELECT lower(type) FROM pragma_table_info($1) WHERE name = $2"
	if dialect == db.Postgres {
		query = `SELECT data_type || coalesce('(' || character_maximum_length || ')', '') FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`
	}
	var got string
	if err := sqlDB.QueryRow(query, table, column).Scan(&got); err != nil {
		t.Fatalf("declared type of %s.%s: %v", table, column, err)
	}
	return got
}

func indexExists(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, name string) bool {
	t.Helper()
	query := "SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = $1"
	if dialect == db.Postgres {
		query = "SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1"
	}
	var n int
	if err := sqlDB.QueryRow(query, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func boundedTitleFixture(t *testing.T, length int) (*sql.DB, db.Dialect) {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	mustApply(t, sqlDB, dialect, CreateTable{Table: "headline", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "varchar", Length: length},
		{Name: "slug", Type: "text", Unique: true},
		{Name: "views", Type: "integer", Indexed: true},
	}})
	if _, err := sqlDB.Exec("INSERT INTO headline (title, slug, views) VALUES ('héllo', 'a', 1), ('wörld', 'b', 2), (NULL, 'c', 3)"); err != nil {
		t.Fatal(err)
	}
	return sqlDB, dialect
}

func assertWidened(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) {
	t.Helper()
	rows, err := sqlDB.Query("SELECT title, slug, views FROM headline ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var title sql.NullString
		var slug string
		var views int
		if err := rows.Scan(&title, &slug, &views); err != nil {
			t.Fatal(err)
		}
		got = append(got, title.String+"|"+slug)
	}
	if want := []string{"héllo|a", "wörld|b", "|c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if _, err := sqlDB.Exec("INSERT INTO headline (title, slug, views) VALUES ('x', 'a', 9)"); err == nil {
		t.Error("the unique column no longer rejects a duplicate")
	}
	if !indexExists(t, sqlDB, dialect, "idx_headline_views") {
		t.Error("the plain index on views was lost")
	}
}

func TestApplyStepWidensAVarcharColumnPreservingEverythingElse(t *testing.T) {
	sqlDB, dialect := boundedTitleFixture(t, 5)
	mustApply(t, sqlDB, dialect, AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 5, To: "varchar", ToLength: 12})

	want := "varchar(12)"
	if dialect == db.Postgres {
		want = "character varying(12)"
	}
	if got := declaredType(t, sqlDB, dialect, "headline", "title"); got != want {
		t.Fatalf("declared type = %q, want %q", got, want)
	}
	assertWidened(t, sqlDB, dialect)
	if dialect == db.Postgres {
		if _, err := sqlDB.Exec("INSERT INTO headline (title, slug, views) VALUES ('twelve chars', 'd', 4)"); err != nil {
			t.Fatalf("a value of the new length: %v", err)
		}
	}
}

func TestApplyStepTurnsAVarcharColumnBackIntoText(t *testing.T) {
	sqlDB, dialect := boundedTitleFixture(t, 5)
	mustApply(t, sqlDB, dialect, AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 5, To: "text"})

	if got := declaredType(t, sqlDB, dialect, "headline", "title"); got != "text" {
		t.Fatalf("declared type = %q, want text", got)
	}
	assertWidened(t, sqlDB, dialect)
	if _, err := sqlDB.Exec("INSERT INTO headline (title, slug, views) VALUES ('a much longer title than five', 'd', 4)"); err != nil {
		t.Fatalf("a long value in a text column: %v", err)
	}
}

func TestApplyStepRefusesAChangeThatIsNeitherWideningNorNarrowing(t *testing.T) {
	steps := []AlterColumnType{
		{Table: "headline", Column: "title", From: "varchar", FromLength: 5, To: "varchar", ToLength: 5},
		{Table: "headline", Column: "title", From: "integer", To: "varchar", ToLength: 9},
		{Table: "headline", Column: "title", From: "varchar", FromLength: 5, To: "integer"},
	}
	for _, step := range steps {
		sqlDB, dialect := boundedTitleFixture(t, 5)
		declaredBefore := declaredType(t, sqlDB, dialect, "headline", "title")
		err := ApplyStep(context.Background(), sqlDB, dialect, step)
		if err == nil || !strings.Contains(err.Error(), "headline.title") || !strings.Contains(err.Error(), "widening") {
			t.Errorf("ApplyStep(%+v) error = %v, want a widening refusal naming headline.title", step, err)
		}
		if got := declaredType(t, sqlDB, dialect, "headline", "title"); got != declaredBefore {
			t.Errorf("the refused step changed the column to %q", got)
		}
	}
}

package migration

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// narrowingModels is the headline model with its text columns bounded as
// lengths says (0 leaves a column unbounded).
func narrowingModels(pk Column, lengths map[string]int) []Model {
	columns := []Column{pk}
	fields := map[string]string{pk.Name: "ID"}
	for _, name := range []string{"title", "slug"} {
		if pk.Name == name {
			continue
		}
		column := Column{Name: name, Type: "text"}
		if n := lengths[name]; n > 0 {
			column = Column{Name: name, Type: "varchar", Length: n}
		}
		columns = append(columns, column)
		fields[name] = strings.ToUpper(name[:1]) + name[1:]
	}
	return []Model{{App: "news", Name: "headline", Struct: "Headline", Fields: fields, Columns: columns}}
}

var idKey = Column{Name: "id", Type: "integer", PrimaryKey: true}

// baseHeadlines is the migration that creates the table with models.
func baseHeadlines(models []Model) Migration {
	return Migration{App: "news", Name: "0001_initial", Reversible: true,
		Up:   []Step{CreateTable{Table: "headline", Columns: models[0].Columns}},
		Down: []Step{DropTable{Table: "headline"}},
	}
}

// generated is the migration makemigrations would write to take base's
// schema to models.
func generated(t *testing.T, base Migration, models []Model) Migration {
	t.Helper()
	state, err := Replay([]Migration{base})
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := DiffModels(models, state)
	if err != nil || len(migrations) != 1 {
		t.Fatalf("DiffModels = %v, %v; want one migration", migrations, err)
	}
	migrations[0].Name = "0002_change"
	return migrations[0]
}

func applyAll(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, migrations ...Migration) error {
	t.Helper()
	return ApplyPending(context.Background(), sqlDB, dialect, migrations)
}

func titles(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()
	rows, err := sqlDB.Query("SELECT CAST(title AS TEXT) FROM headline ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var title sql.NullString
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		got = append(got, title.String)
	}
	return got
}

func TestDiffClassifiesNarrowingsAsReversibleWithAWideningDown(t *testing.T) {
	tests := []struct {
		name       string
		base, next map[string]int
		wantUp     AlterColumnType
		wantDown   AlterColumnType
	}{
		{"text to varchar", nil, map[string]int{"title": 5},
			AlterColumnType{Table: "headline", Column: "title", From: "text", To: "varchar", ToLength: 5},
			AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 5, To: "text"}},
		{"a shorter varchar", map[string]int{"title": 10}, map[string]int{"title": 5},
			AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 10, To: "varchar", ToLength: 5},
			AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 5, To: "varchar", ToLength: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := baseHeadlines(narrowingModels(idKey, tt.base))
			m := generated(t, base, narrowingModels(idKey, tt.next))
			want := Migration{App: "news", Name: "0002_change", Reversible: true, Up: []Step{tt.wantUp}, Down: []Step{tt.wantDown}}
			if !reflect.DeepEqual(m, want) {
				t.Fatalf("migration = %#v, want %#v", m, want)
			}
		})
	}
}

func TestDiffKeepsAWideningMigrationIrreversibleWithoutADown(t *testing.T) {
	base := baseHeadlines(narrowingModels(idKey, map[string]int{"title": 5}))
	for _, next := range []map[string]int{nil, {"title": 9}} {
		m := generated(t, base, narrowingModels(idKey, next))
		if m.Reversible || len(m.Down) != 0 {
			t.Errorf("widening to %v: Reversible=%v Down=%v, want irreversible with no Down", next, m.Reversible, m.Down)
		}
	}
}

func TestMixedNarrowingAndWideningIsIrreversibleAsAWhole(t *testing.T) {
	base := baseHeadlines(narrowingModels(idKey, map[string]int{"slug": 5}))
	m := generated(t, base, narrowingModels(idKey, map[string]int{"title": 4, "slug": 9}))
	if len(m.Up) != 2 {
		t.Fatalf("Up = %v, want a narrowing and a widening", m.Up)
	}
	if m.Reversible || len(m.Down) != 0 {
		t.Fatalf("Reversible=%v Down=%v, want irreversible with no partial Down", m.Reversible, m.Down)
	}

	sqlDB, dialect := testdb.Open(t)
	if err := applyAll(t, sqlDB, dialect, base, m); err != nil {
		t.Fatal(err)
	}
	if err := RollbackLast(context.Background(), sqlDB, dialect, []Migration{base, m}); !errors.Is(err, ErrIrreversibleMigration) {
		t.Fatalf("RollbackLast error = %v, want ErrIrreversibleMigration", err)
	}
}

func TestSeveralNarrowingsStayReversibleWithAllTheirWideningDowns(t *testing.T) {
	base := baseHeadlines(narrowingModels(idKey, nil))
	m := generated(t, base, narrowingModels(idKey, map[string]int{"title": 4, "slug": 6}))
	if !m.Reversible || len(m.Up) != 2 || len(m.Down) != 2 {
		t.Fatalf("migration = %#v, want reversible with two Up and two Down steps", m)
	}
	for _, step := range m.Down {
		if s := step.(AlterColumnType); s.From != "varchar" || s.To != "text" {
			t.Errorf("Down step %#v does not widen to text", s)
		}
	}
}

func TestNarrowingAppliesWhenEveryValueFitsAndRollsBack(t *testing.T) {
	for _, tt := range []struct {
		name       string
		base, next map[string]int
		wantBefore string
		wantAfter  string
	}{
		{"text to varchar", nil, map[string]int{"title": 5}, "text", "varchar(5)"},
		{"a shorter varchar", map[string]int{"title": 9}, map[string]int{"title": 5}, "varchar(9)", "varchar(5)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, dialect := testdb.Open(t)
			base := baseHeadlines(narrowingModels(idKey, tt.base))
			if err := applyAll(t, sqlDB, dialect, base); err != nil {
				t.Fatal(err)
			}
			// "héllo" is five runes and six bytes: it fits varchar(5).
			if _, err := sqlDB.Exec("INSERT INTO headline (title, slug) VALUES ('héllo', 'a'), ('日本語', 'b'), (NULL, 'c'), ('', 'd')"); err != nil {
				t.Fatal(err)
			}
			want := []string{"héllo", "日本語", "", ""}
			narrow := generated(t, base, narrowingModels(idKey, tt.next))
			if err := applyAll(t, sqlDB, dialect, base, narrow); err != nil {
				t.Fatalf("narrowing: %v", err)
			}
			if got := titles(t, sqlDB); !reflect.DeepEqual(got, want) {
				t.Fatalf("values after narrowing = %q, want %q", got, want)
			}
			assertDeclared(t, sqlDB, dialect, tt.wantAfter)

			if err := RollbackLast(context.Background(), sqlDB, dialect, []Migration{base, narrow}); err != nil {
				t.Fatalf("rollback: %v", err)
			}
			if got := titles(t, sqlDB); !reflect.DeepEqual(got, want) {
				t.Fatalf("values after rollback = %q, want %q", got, want)
			}
			assertDeclared(t, sqlDB, dialect, tt.wantBefore)
		})
	}
}

// assertDeclared checks the title column's declared type in a dialect-neutral
// spelling: "text" or "varchar(n)".
func assertDeclared(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, want string) {
	t.Helper()
	got := declaredType(t, sqlDB, dialect, "headline", "title")
	got = strings.Replace(got, "character varying", "varchar", 1)
	if got != want {
		t.Fatalf("declared type = %q, want %q", got, want)
	}
}

func TestNarrowingFailsBeforeAnyDDLWhenAValueIsTooLong(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	base := baseHeadlines(narrowingModels(idKey, nil))
	if err := applyAll(t, sqlDB, dialect, base); err != nil {
		t.Fatal(err)
	}
	// Both too-long values are 6 runes; 'héllo!' is 7 bytes, and 'ok' fits.
	if _, err := sqlDB.Exec("INSERT INTO headline (title, slug) VALUES ('ok', 'a'), ('héllo!', 'b'), ('日本語日本語', 'c')"); err != nil {
		t.Fatal(err)
	}
	narrow := generated(t, base, narrowingModels(idKey, map[string]int{"title": 5}))

	err := applyAll(t, sqlDB, dialect, base, narrow)
	if err == nil {
		t.Fatal("narrowing below the longest value succeeded")
	}
	for _, want := range []string{"headline.title", "2 rows", "id = 2", "varchar(5)", "shorten"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if got := titles(t, sqlDB); !reflect.DeepEqual(got, []string{"ok", "héllo!", "日本語日本語"}) {
		t.Fatalf("data after the failure = %q, want it unchanged", got)
	}
	assertDeclared(t, sqlDB, dialect, "text")
	applied, aerr := AppliedMigrations(context.Background(), sqlDB)
	if aerr != nil || applied[MigrationKey{App: "news", Name: "0002_change"}] {
		t.Fatalf("the failed migration was recorded as applied (%v, %v)", applied, aerr)
	}
}

// A text primary key not named id: the preflight reports that row's own key.
func TestNarrowingPreflightFindsTheRealPrimaryKey(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	key := Column{Name: "slug", Type: "text", PrimaryKey: true}
	base := baseHeadlines(narrowingModels(key, nil))
	if err := applyAll(t, sqlDB, dialect, base); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec("INSERT INTO headline (slug, title) VALUES ('first', 'fine'), ('second', 'far too long')"); err != nil {
		t.Fatal(err)
	}
	narrow := generated(t, base, narrowingModels(key, map[string]int{"title": 5}))

	err := applyAll(t, sqlDB, dialect, base, narrow)
	if err == nil || !strings.Contains(err.Error(), `slug = "second"`) || !strings.Contains(err.Error(), "1 row") {
		t.Fatalf("error = %v, want one naming slug = \"second\" and 1 row", err)
	}
	var count int
	if err := sqlDB.QueryRow("SELECT count(*) FROM headline WHERE title = 'far too long'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("the data changed (count %d, %v)", count, err)
	}
	assertDeclared(t, sqlDB, dialect, "text")
}

func TestRollingBackAWideningIsRefusedAndNarrowingBackWouldFail(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	base := baseHeadlines(narrowingModels(idKey, map[string]int{"title": 5}))
	widen := generated(t, base, narrowingModels(idKey, nil))
	if err := applyAll(t, sqlDB, dialect, base, widen); err != nil {
		t.Fatal(err)
	}

	err := RollbackLast(context.Background(), sqlDB, dialect, []Migration{base, widen})
	if !errors.Is(err, ErrIrreversibleMigration) || !strings.Contains(err.Error(), "narrowing migration") {
		t.Fatalf("RollbackLast error = %v, want ErrIrreversibleMigration pointing to a narrowing migration", err)
	}

	if _, err := sqlDB.Exec("INSERT INTO headline (title, slug) VALUES ('written after widening', 'a')"); err != nil {
		t.Fatal(err)
	}
	reverse := AlterColumnType{Table: "headline", Column: "title", From: "text", To: "varchar", ToLength: 5}
	if err := ApplyStep(context.Background(), sqlDB, dialect, reverse); err == nil || !strings.Contains(err.Error(), "1 row") {
		t.Fatalf("narrowing back = %v, want the preflight to refuse the new value", err)
	}
}

func TestIrreversibleMigrationHasNoDownAtAll(t *testing.T) {
	base := baseHeadlines(narrowingModels(idKey, map[string]int{"title": 5}))
	models := narrowingModels(idKey, nil) // widens title to text...
	models[0].Columns = append(models[0].Columns, Column{Name: "extra", Type: "text", Indexed: true})
	models[0].Fields["extra"] = "Extra" // ...and adds an indexed column, each with a reverse of its own
	m := generated(t, base, models)
	if m.Reversible {
		t.Fatal("a widening migration is marked reversible")
	}
	if len(m.Down) != 0 {
		t.Fatalf("Down = %#v, want no steps at all for an irreversible migration", m.Down)
	}
}

func columnNamesOf(t *testing.T, sqlDB *sql.DB, dialect db.Dialect) []string {
	t.Helper()
	return columnNames(t, sqlDB, dialect, "headline")
}

func TestFailedNarrowingLeavesTheSchemaOfEarlierStepsUnchanged(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	base := baseHeadlines(narrowingModels(idKey, nil))
	if err := applyAll(t, sqlDB, dialect, base); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec("INSERT INTO headline (title, slug) VALUES ('far too long', 'a')"); err != nil {
		t.Fatal(err)
	}
	before := columnNamesOf(t, sqlDB, dialect)
	m := Migration{App: "news", Name: "0002_change", Up: []Step{
		AddColumn{Table: "headline", Column: Column{Name: "extra", Type: "text"}},
		CreateIndex{Table: "headline", Column: "slug"},
		AlterColumnType{Table: "headline", Column: "title", From: "text", To: "varchar", ToLength: 5},
	}}
	err := applyAll(t, sqlDB, dialect, base, m)
	if err == nil || !strings.Contains(err.Error(), "1 row") {
		t.Fatalf("error = %v, want the narrowing refusal", err)
	}
	if after := columnNamesOf(t, sqlDB, dialect); !reflect.DeepEqual(after, before) {
		t.Fatalf("columns = %v after the failure, want %v", after, before)
	}
	if indexExists(t, sqlDB, dialect, "idx_headline_slug") {
		t.Fatal("an earlier CreateIndex survived the failed migration")
	}
	if got := titles(t, sqlDB); !reflect.DeepEqual(got, []string{"far too long"}) {
		t.Fatalf("data = %q", got)
	}
	applied, _ := AppliedMigrations(context.Background(), sqlDB)
	if applied[MigrationKey{App: "news", Name: "0002_change"}] {
		t.Fatal("the failed migration was recorded")
	}
}

func TestNarrowingPreflightSeesPhysicalNamesBeforeARenameInTheSameMigration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		title   string
		wantErr bool
	}{{"fits", "short", false}, {"too long", "far too long", true}} {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, dialect := testdb.Open(t)
			base := baseHeadlines(narrowingModels(idKey, nil))
			if err := applyAll(t, sqlDB, dialect, base); err != nil {
				t.Fatal(err)
			}
			if _, err := sqlDB.Exec("INSERT INTO headline (title, slug) VALUES ($1, 'a')", tt.title); err != nil {
				t.Fatal(err)
			}
			m := Migration{App: "news", Name: "0002_change", Up: []Step{
				RenameTable{From: "headline", To: "article"},
				RenameColumn{Table: "article", From: "title", To: "heading"},
				AlterColumnType{Table: "article", Column: "heading", From: "text", To: "varchar", ToLength: 5},
			}}
			err := applyAll(t, sqlDB, dialect, base, m)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "1 row") || !strings.Contains(err.Error(), "headline.title") {
					t.Fatalf("error = %v, want a refusal naming the physical headline.title", err)
				}
				if !tableExists(t, sqlDB, dialect, "headline") || tableExists(t, sqlDB, dialect, "article") {
					t.Fatal("the rename ran although the narrowing was refused")
				}
				return
			}
			if err != nil {
				t.Fatalf("rename plus narrowing: %v", err)
			}
		})
	}
}

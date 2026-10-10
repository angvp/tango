package migration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

func stringField(name string, maxLength int) model.FieldMeta {
	return model.FieldMeta{Name: name, Type: reflect.TypeOf(""), MaxLength: maxLength}
}

func headlineMeta(fields ...model.FieldMeta) []model.ModelMeta {
	id := model.FieldMeta{Name: "ID", Type: reflect.TypeOf(int64(0)), PrimaryKey: true}
	return []model.ModelMeta{{App: "news", Name: "Headline", Fields: append([]model.FieldMeta{id}, fields...)}}
}

func TestDiffGivesABoundedStringAVarcharColumnWithItsLength(t *testing.T) {
	migrations, err := Diff(headlineMeta(stringField("Title", 200)), SchemaState{Tables: map[string]TableState{}})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	want := []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "varchar", Length: 200},
	}
	create, ok := migrations[0].Up[0].(CreateTable)
	if !ok || !reflect.DeepEqual(create.Columns, want) {
		t.Fatalf("Up[0] = %#v, want CreateTable with columns %#v", migrations[0].Up[0], want)
	}
}

func TestDiffAddsABoundedColumnToAnExistingTable(t *testing.T) {
	state := SchemaState{Tables: map[string]TableState{"headline": {Name: "headline", App: "news", Columns: []ColumnState{
		{Name: "id", Type: "integer", PrimaryKey: true},
	}}}}
	migrations, err := Diff(headlineMeta(stringField("Slug", 5)), state)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	want := AddColumn{Table: "headline", Column: Column{Name: "slug", Type: "varchar", Length: 5}}
	if got := migrations[0].Up[0]; got != want {
		t.Fatalf("Up[0] = %#v, want %#v", got, want)
	}
}

func TestReplayKeepsTheLengthOfBoundedColumns(t *testing.T) {
	state, err := Replay([]Migration{
		{App: "news", Name: "0001", Up: []Step{CreateTable{Table: "headline", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "title", Type: "varchar", Length: 200},
		}}}},
		{App: "news", Name: "0002", Up: []Step{AddColumn{Table: "headline", Column: Column{Name: "slug", Type: "varchar", Length: 5}}}},
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	columns := state.Tables["headline"].Columns
	if columns[1].Length != 200 || columns[2].Length != 5 {
		t.Fatalf("lengths = %d, %d; want 200, 5", columns[1].Length, columns[2].Length)
	}
}

// A model that replays to its own state yields nothing to migrate: the
// round trip generate, replay, regenerate must be quiet.
func TestBoundedColumnsProduceNoDiffAfterReplay(t *testing.T) {
	meta := headlineMeta(stringField("Title", 200), stringField("Body", 0))
	first, err := Diff(meta, SchemaState{Tables: map[string]TableState{}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Diff(meta, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("regenerating produced %#v, want nothing", again)
	}
}

func TestBareStringStaysATextColumnWithoutALength(t *testing.T) {
	migrations, err := Diff(headlineMeta(stringField("Body", 0)), SchemaState{Tables: map[string]TableState{}})
	if err != nil {
		t.Fatal(err)
	}
	create := migrations[0].Up[0].(CreateTable)
	if got := create.Columns[1]; got != (Column{Name: "body", Type: "text"}) {
		t.Fatalf("body = %#v, want a plain text column", got)
	}
}

func TestDiffRefusesLengthChangesUntilTheyAreSupported(t *testing.T) {
	tests := []struct {
		name string
		from ColumnState
		to   int
	}{
		{"text to varchar", ColumnState{Name: "title", Type: "text"}, 200},
		{"varchar to a shorter varchar", ColumnState{Name: "title", Type: "varchar", Length: 300}, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := SchemaState{Tables: map[string]TableState{"headline": {Name: "headline", App: "news", Columns: []ColumnState{
				{Name: "id", Type: "integer", PrimaryKey: true}, tt.from,
			}}}}
			_, err := Diff(headlineMeta(stringField("Title", tt.to)), state)
			if !errors.Is(err, ErrUnsupportedChange) || !strings.Contains(err.Error(), "news.Headline.Title") {
				t.Fatalf("Diff error = %v, want ErrUnsupportedChange naming news.Headline.Title", err)
			}
		})
	}
}

func TestVarcharDDLOnBothDialects(t *testing.T) {
	step := CreateTable{Table: "headline", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "varchar", Length: 200},
	}}
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		got := createTableSQL(dialect, step)[0]
		if !strings.Contains(got, "VARCHAR(200)") || strings.Contains(strings.ToUpper(got), "CHECK") {
			t.Errorf("%v DDL = %s, want VARCHAR(200) and no CHECK", dialect, got)
		}
	}
}

func TestApplyStepCreatesAndAddsBoundedColumns(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	mustApply(t, sqlDB, dialect, CreateTable{Table: "headline", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "varchar", Length: 5},
	}})
	mustApply(t, sqlDB, dialect, AddColumn{Table: "headline", Column: Column{Name: "slug", Type: "varchar", Length: 3}})

	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO headline (title, slug) VALUES ('hello', 'abc')"); err != nil {
		t.Fatalf("a value of exactly the length: %v", err)
	}
	if dialect == db.Postgres {
		if _, err := sqlDB.ExecContext(ctx, "INSERT INTO headline (title, slug) VALUES ('toolong', 'abc')"); err == nil {
			t.Fatal("PostgreSQL accepted an over-long value in raw SQL")
		}
	}
}

func TestInvalidVarcharStatesFailLoudly(t *testing.T) {
	bad := []Column{
		{Name: "title", Type: "varchar"},
		{Name: "title", Type: "varchar", Length: -1},
		{Name: "title", Type: "varchar", Length: 10485761},
		{Name: "title", Type: "text", Length: 5},
		{Name: "title", Type: "integer", Length: 5},
	}
	for _, column := range bad {
		steps := map[string]Step{
			"create": CreateTable{Table: "headline", Columns: []Column{column}},
			"add":    AddColumn{Table: "headline", Column: column},
		}
		for kind, step := range steps {
			t.Run(kind+"/"+column.Type, func(t *testing.T) {
				sqlDB, dialect := testdb.Open(t)
				if err := ApplyStep(context.Background(), sqlDB, dialect, step); err == nil || !strings.Contains(err.Error(), "headline") || !strings.Contains(err.Error(), "title") {
					t.Fatalf("ApplyStep error = %v, want one naming headline.title", err)
				}
				if _, err := Replay([]Migration{{App: "news", Name: "0001", Up: []Step{
					CreateTable{Table: "headline", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
					AddColumn{Table: "headline", Column: column},
				}}}); err == nil {
					t.Fatal("Replay accepted an invalid column")
				}
			})
		}
	}
}

func TestUnknownLegacyTypeTokensStillFallBackToText(t *testing.T) {
	if got := baseTypeSQL(db.SQLite, "uuid", 0); got != "TEXT" {
		t.Fatalf("legacy token = %q, want TEXT", got)
	}
}

func TestReplayTakesTheLengthFromAnAlterColumnTypeTarget(t *testing.T) {
	create := Migration{App: "news", Name: "0001", Up: []Step{CreateTable{Table: "headline", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "varchar", Length: 100},
		{Name: "views", Type: "integer"},
	}}}}
	alter := Migration{App: "news", Name: "0002", Up: []Step{
		AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 100, To: "text"},
		AlterColumnType{Table: "headline", Column: "views", From: "integer", To: "text"},
	}}
	state, err := Replay([]Migration{create, alter})
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range state.Tables["headline"].Columns {
		if column.Length != 0 {
			t.Errorf("%s keeps length %d after becoming %s", column.Name, column.Length, column.Type)
		}
	}

	grow := Migration{App: "news", Name: "0003", Up: []Step{
		AlterColumnType{Table: "headline", Column: "title", From: "text", To: "varchar", ToLength: 50},
	}}
	state, err = Replay([]Migration{create, alter, grow})
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Tables["headline"].Columns[1]; got.Type != "varchar" || got.Length != 50 {
		t.Fatalf("title = %#v, want varchar(50)", got)
	}
}

func TestInvalidAlterColumnTypeLengthsFailReplay(t *testing.T) {
	bad := []AlterColumnType{
		{Table: "headline", Column: "title", From: "text", To: "varchar"},
		{Table: "headline", Column: "title", From: "text", To: "varchar", ToLength: -3},
		{Table: "headline", Column: "title", From: "text", To: "varchar", ToLength: 10485761},
		{Table: "headline", Column: "title", From: "varchar", To: "text", ToLength: 5},
		{Table: "headline", Column: "title", From: "text", FromLength: 5, To: "varchar", ToLength: 5},
	}
	for _, step := range bad {
		_, err := Replay([]Migration{{App: "news", Name: "0001", Up: []Step{
			CreateTable{Table: "headline", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "title", Type: "text"}}},
			step,
		}}})
		if err == nil || !strings.Contains(err.Error(), "headline.title") {
			t.Errorf("Replay(%+v) error = %v, want one naming headline.title", step, err)
		}
		sqlDB, dialect := testdb.Open(t)
		if err := ApplyStep(context.Background(), sqlDB, dialect, step); err == nil || !strings.Contains(err.Error(), "headline.title") {
			t.Errorf("ApplyStep(%+v) error = %v, want one naming headline.title", step, err)
		}
	}
}

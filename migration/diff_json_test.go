package migration

import (
	"reflect"
	"testing"

	"github.com/angvp/tango/model"
)

type diffJSONDoc struct {
	ID    int64  `tango:"pk"`
	Prefs string `tango:"json"`
}

// A JSON field is ordinary text to the schema: a table that already has the
// column as untagged text needs no migration when the tag is added.
func TestDiffAddingTheJSONTagToAnExistingTextColumnProducesNoMigration(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "docs", Up: []Step{
			CreateTable{Table: "diff_json_doc", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "prefs", Type: "text"},
			}},
		}},
	})

	meta := registerDiffModel(t, "docs", diffJSONDoc{})
	if migrations := mustDiff(t, []model.ModelMeta{meta}, state); len(migrations) != 0 {
		t.Fatalf("got %d migrations, want 0: %+v", len(migrations), migrations)
	}
}

func TestDiffNewJSONFieldCreatesAnOrdinaryTextColumn(t *testing.T) {
	meta := registerDiffModel(t, "docs", diffJSONDoc{})
	migrations := mustDiff(t, []model.ModelMeta{meta}, SchemaState{Tables: map[string]TableState{}})
	create, ok := migrations[0].Up[0].(CreateTable)
	if !ok {
		t.Fatalf("step type = %T, want CreateTable", migrations[0].Up[0])
	}
	want := []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "prefs", Type: "text"},
	}
	if !reflect.DeepEqual(create.Columns, want) {
		t.Fatalf("columns = %+v, want %+v", create.Columns, want)
	}
}

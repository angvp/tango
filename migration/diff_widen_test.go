package migration

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// sampleState is history with a shop app's sample table holding one
// column v of type from, with def as its default ("" for none).
func sampleState(from, def string) SchemaState {
	return SchemaState{Tables: map[string]TableState{
		"sample": {Name: "sample", App: "shop", Columns: []ColumnState{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "v", Type: from, Default: def},
		}},
	}}
}

// sampleModels is the sample model with v of type to, named vName.
func sampleModels(to, vName string) []Model {
	return []Model{{
		App: "shop", Name: "sample", Struct: "Sample",
		Fields:  map[string]string{"id": "ID", vName: "V"},
		Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: vName, Type: to}},
	}}
}

func TestDiffTurnsAWideningTypeChangeIntoAlterColumnType(t *testing.T) {
	tests := []struct {
		from, to, def, wantDefault string
	}{
		{"integer", "real", "", ""},
		{"integer", "real", "7", "7.0"},
		{"integer", "text", "-7", "'-7'"},
		{"real", "text", "2.5", "'2.5'"},
		{"real", "text", "1e20", "'1e+20'"},
		{"real", "text", "3.0", "'3'"},
		{"boolean", "integer", "TRUE", "1"},
		{"boolean", "integer", "false", "0"},
		{"boolean", "text", "FALSE", "'false'"},
	}
	for _, tt := range tests {
		t.Run(tt.from+"_"+tt.to+"_"+tt.def, func(t *testing.T) {
			migrations, err := DiffModels(sampleModels(tt.to, "v"), sampleState(tt.from, tt.def))
			if err != nil {
				t.Fatalf("DiffModels: %v", err)
			}
			want := []Migration{{
				App: "shop",
				Up:  []Step{AlterColumnType{Table: "sample", Column: "v", From: tt.from, To: tt.to, Default: tt.wantDefault}},
			}}
			if !reflect.DeepEqual(migrations, want) {
				t.Fatalf("migrations = %#v, want %#v (irreversible)", migrations, want)
			}
		})
	}
}

func TestDiffRenamesBeforeWideningAndStaysIrreversible(t *testing.T) {
	migrations, err := DiffModels(sampleModels("real", "amount"), sampleState("integer", "7"),
		Rename{App: "shop", Table: "sample", Column: "v", To: "amount"})
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	wantUp := []Step{
		RenameColumn{Table: "sample", From: "v", To: "amount"},
		AlterColumnType{Table: "sample", Column: "amount", From: "integer", To: "real", Default: "7.0"},
	}
	if len(migrations) != 1 || !reflect.DeepEqual(migrations[0].Up, wantUp) || migrations[0].Reversible {
		t.Fatalf("migrations = %#v, want one irreversible migration with Up %#v", migrations, wantUp)
	}
}

func TestDiffRefusesTypeChangesThatAreNotWidening(t *testing.T) {
	tests := []struct {
		name  string
		state SchemaState
		model []Model
		wants []string
	}{
		{"narrowing", sampleState("text", ""), sampleModels("integer", "v"), []string{"shop.Sample.V", "text", "integer"}},
		{"timestamp", sampleState("timestamp", ""), sampleModels("text", "v"), []string{"shop.Sample.V", "timestamp", "text"}},
		{"default that does not convert", sampleState("boolean", "CURRENT_TIMESTAMP"), sampleModels("integer", "v"), []string{"shop.Sample.V", "CURRENT_TIMESTAMP"}},
		{"primary key type", SchemaState{Tables: map[string]TableState{"sample": {Name: "sample", App: "shop", Columns: []ColumnState{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}}}}, []Model{{App: "shop", Name: "sample", Struct: "Sample", Fields: map[string]string{"id": "ID"}, Columns: []Column{
			{Name: "id", Type: "text", PrimaryKey: true},
		}}}, []string{"shop.Sample.ID", "primary key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migrations, err := DiffModels(tt.model, tt.state)
			if !errors.Is(err, ErrUnsupportedChange) || migrations != nil {
				t.Fatalf("DiffModels = %#v, %v; want ErrUnsupportedChange and no migrations", migrations, err)
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestReplayTracksDefaultsAndTypeChanges(t *testing.T) {
	state, err := Replay([]Migration{
		{App: "shop", Name: "0001", Up: []Step{CreateTable{Table: "sample", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}}},
		{App: "shop", Name: "0002", Up: []Step{AddColumn{Table: "sample", Column: Column{Name: "v", Type: "boolean", Default: "TRUE"}}}},
		{App: "shop", Name: "0003", Up: []Step{
			RenameColumn{Table: "sample", From: "v", To: "flag"},
			AlterColumnType{Table: "sample", Column: "flag", From: "boolean", To: "integer", Default: "1"},
		}},
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	want := ColumnState{Name: "flag", Type: "integer", Default: "1"}
	if got := state.Tables["sample"].Columns[1]; got != want {
		t.Fatalf("replayed column = %#v, want %#v", got, want)
	}
	again, err := DiffModels(sampleModels("integer", "flag"), state)
	if err != nil || len(again) != 0 {
		t.Fatalf("DiffModels after the type change = %#v, %v; want no migrations", again, err)
	}
}

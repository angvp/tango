package migration

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

func tableT() CreateTable {
	return CreateTable{Table: "t", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "a", Type: "integer"},
		{Name: "b", Type: "text"},
		{Name: "c", Type: "text"},
	}}
}

func replayUp(t *testing.T, steps ...Step) SchemaState {
	t.Helper()
	state, err := Replay([]Migration{{App: "app", Name: "0001_test", Up: steps}})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return state
}

func TestReplayTracksCompositeIndexes(t *testing.T) {
	testdb.SQLiteOnly(t, "pure replay test")
	state := replayUp(t, tableT(),
		CreateCompositeIndex{Table: "t", Columns: []string{"b", "a"}},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true, Name: "ix_ab"},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "c"}, Name: "idx_t_a_c"},
		DropCompositeIndex{Table: "t", Columns: []string{"b", "a"}},
	)
	want := []CompositeIndex{
		{Columns: []string{"a", "c"}},
		{Columns: []string{"a", "b"}, Unique: true, Name: "ix_ab"},
	}
	if got := state.Tables["t"].Indexes; !reflect.DeepEqual(got, want) {
		t.Fatalf("Indexes = %#v, want %#v (a name equal to the derived one is stored as none)", got, want)
	}
}

func TestReplayRefusesInvalidCompositeSteps(t *testing.T) {
	testdb.SQLiteOnly(t, "pure replay test")
	exists := CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}}
	cases := map[string]struct {
		steps []Step
		want  string
	}{
		"unknown table":       {[]Step{CreateCompositeIndex{Table: "nope", Columns: []string{"a", "b"}}}, `table "nope" does not exist`},
		"unknown column":      {[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "zzz"}}}, `column "zzz" does not exist`},
		"one column":          {[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a"}}}, "at least two columns"},
		"repeated column":     {[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "a"}}}, `column "a" is repeated`},
		"bad name":            {[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Name: "bad-name"}}, "bad-name"},
		"name taken":          {[]Step{tableT(), exists, CreateCompositeIndex{Table: "t", Columns: []string{"a", "c"}, Name: "idx_t_a_b"}}, `index "idx_t_a_b" already exists`},
		"drop absent":         {[]Step{tableT(), DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}}}, `index "idx_t_a_b" does not exist`},
		"drop wrong tuple":    {[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Name: "ix"}, DropCompositeIndex{Table: "t", Columns: []string{"a", "c"}, Name: "ix"}}, `does not cover`},
		"rename absent":       {[]Step{tableT(), RenameCompositeIndex{Table: "t", Columns: []string{"a", "b"}, From: "ix", To: "iy"}}, `index "ix" does not exist`},
		"rename onto another": {[]Step{tableT(), exists, CreateCompositeIndex{Table: "t", Columns: []string{"a", "c"}}, RenameCompositeIndex{Table: "t", Columns: []string{"a", "c"}, From: "idx_t_a_c", To: "idx_t_a_b"}}, `index "idx_t_a_b" already exists`},
		"drop column in one":  {[]Step{tableT(), exists, DropColumn{Table: "t", Column: "b"}}, `index "idx_t_a_b" still contains it`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Replay([]Migration{{App: "app", Name: "0001_test", Up: tc.steps}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Replay error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestReplayKeepsThePhysicalNameThroughRenames(t *testing.T) {
	testdb.SQLiteOnly(t, "pure replay test")
	state := replayUp(t, tableT(),
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "c"}, Name: "ix_ac"},
		RenameColumn{Table: "t", From: "a", To: "x"},
	)
	want := []CompositeIndex{
		{Columns: []string{"x", "b"}, Name: "idx_t_a_b"},
		{Columns: []string{"x", "c"}, Name: "ix_ac"},
	}
	if got := state.Tables["t"].Indexes; !reflect.DeepEqual(got, want) {
		t.Fatalf("after a column rename Indexes = %#v, want %#v: the derived name stays the physical one until it is renamed", got, want)
	}
	state = replayUp(t, tableT(),
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		RenameTable{From: "t", To: "u"},
	)
	want = []CompositeIndex{{Columns: []string{"a", "b"}, Name: "idx_t_a_b"}}
	if got := state.Tables["u"].Indexes; !reflect.DeepEqual(got, want) {
		t.Fatalf("after a table rename Indexes = %#v, want %#v", got, want)
	}
	state = replayUp(t, tableT(),
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		RenameColumn{Table: "t", From: "a", To: "x"},
		RenameCompositeIndex{Table: "t", Columns: []string{"x", "b"}, From: "idx_t_a_b", To: "idx_t_x_b"},
	)
	want = []CompositeIndex{{Columns: []string{"x", "b"}}}
	if got := state.Tables["t"].Indexes; !reflect.DeepEqual(got, want) {
		t.Fatalf("after the rename step Indexes = %#v, want the derived name again (%#v)", got, want)
	}
}

// compositeMigration runs DiffModels for one model in app "app" against the
// state the base steps replay to, and checks that applying the result leaves
// nothing to generate.
func compositeMigration(t *testing.T, model Model, base []Step, renames ...Rename) Migration {
	t.Helper()
	model.App, model.Struct = "app", "T"
	state := replayUp(t, base...)
	migrations, err := DiffModels([]Model{model}, state, renames...)
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("DiffModels returned %d migrations, want 1", len(migrations))
	}
	replayed, err := Replay([]Migration{{App: "app", Name: "0001_base", Up: base}, migrations[0]})
	if err != nil {
		t.Fatalf("Replay of the result: %v", err)
	}
	if again, err := DiffModels([]Model{model}, replayed); err != nil || len(again) != 0 {
		t.Fatalf("applying the result left %v (err %v), want nothing to generate", again, err)
	}
	return migrations[0]
}

func modelT(indexes ...CompositeIndex) Model {
	return Model{Name: "t", Columns: tableT().Columns, Indexes: indexes}
}

func TestDiffCreatesCompositeIndexesOfANewTableAfterTheTable(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	model := modelT(
		CompositeIndex{Columns: []string{"b", "a"}},
		CompositeIndex{Columns: []string{"a", "b"}, Unique: true},
	)
	model.App = "app"
	migrations, err := DiffModels([]Model{model}, SchemaState{Tables: map[string]TableState{}})
	if err != nil {
		t.Fatal(err)
	}
	m := migrations[0]
	wantUp := []Step{
		CreateTable{Table: "t", Columns: tableT().Columns},
		CreateCompositeIndex{Table: "t", Columns: []string{"b", "a"}},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true},
	}
	if !reflect.DeepEqual(m.Up, wantUp) {
		t.Fatalf("Up = %#v, want %#v", m.Up, wantUp)
	}
	if !reflect.DeepEqual(m.Down, []Step{DropTable{Table: "t"}}) || !m.Reversible {
		t.Fatalf("Down = %#v, Reversible = %v; want just DropTable, reversible", m.Down, m.Reversible)
	}
}

func TestDiffAddsAndRemovesCompositeIndexesReversibly(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	ab := CompositeIndex{Columns: []string{"a", "b"}}
	added := compositeMigration(t, modelT(ab), []Step{tableT()})
	if want := []Step{CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}}}; !reflect.DeepEqual(added.Up, want) {
		t.Fatalf("Up = %#v, want %#v", added.Up, want)
	}
	if want := []Step{DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}}}; !reflect.DeepEqual(added.Down, want) || !added.Reversible {
		t.Fatalf("Down = %#v, Reversible = %v; want the drop, reversible", added.Down, added.Reversible)
	}
	removed := compositeMigration(t, modelT(), []Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true, Name: "ix_ab"}})
	if want := []Step{DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true, Name: "ix_ab"}}; !reflect.DeepEqual(removed.Up, want) {
		t.Fatalf("Up = %#v, want %#v", removed.Up, want)
	}
	if want := []Step{CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true, Name: "ix_ab"}}; !reflect.DeepEqual(removed.Down, want) || !removed.Reversible {
		t.Fatalf("Down = %#v, Reversible = %v; want the create, reversible", removed.Down, removed.Reversible)
	}
}

func TestDiffOfUnchangedOrReorderedCompositeIndexesIsEmpty(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	base := []Step{tableT(),
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "c"}, Unique: true},
	}
	state := replayUp(t, base...)
	for name, indexes := range map[string][]CompositeIndex{
		"same order":     {{Columns: []string{"a", "b"}}, {Columns: []string{"a", "c"}, Unique: true}},
		"reversed order": {{Columns: []string{"a", "c"}, Unique: true}, {Columns: []string{"a", "b"}}},
	} {
		model := modelT(indexes...)
		model.App = "app"
		migrations, err := DiffModels([]Model{model}, state)
		if err != nil || len(migrations) != 0 {
			t.Fatalf("%s: DiffModels = %v, %v; want nothing", name, migrations, err)
		}
	}
}

func TestDiffTurnsAnIndexIntoAUniqueTupleByDroppingAndCreating(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	m := compositeMigration(t, modelT(CompositeIndex{Columns: []string{"a", "b"}, Unique: true}),
		[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}}})
	wantUp := []Step{
		DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true},
	}
	wantDown := []Step{
		DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Unique: true},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
	}
	if !reflect.DeepEqual(m.Up, wantUp) || !reflect.DeepEqual(m.Down, wantDown) {
		t.Fatalf("Up = %#v\nDown = %#v\nwant\n%#v\n%#v", m.Up, m.Down, wantUp, wantDown)
	}
}

func TestDiffDropsACompositeIndexBeforeTheColumnItUses(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	model := Model{Name: "t", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "a", Type: "integer"},
		{Name: "c", Type: "text"},
	}}
	m := compositeMigration(t, model, []Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}}})
	want := []Step{
		DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		DropColumn{Table: "t", Column: "b"},
	}
	if !reflect.DeepEqual(m.Up, want) {
		t.Fatalf("Up = %#v, want %#v", m.Up, want)
	}
	if m.Reversible {
		t.Fatalf("a migration that drops a column is irreversible, as before")
	}
}

func TestDiffCreatesACompositeIndexAfterTheColumnsItUses(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	model := modelT(CompositeIndex{Columns: []string{"a", "d"}})
	model.Columns = append(append([]Column(nil), model.Columns...), Column{Name: "d", Type: "text"})
	m := compositeMigration(t, model, []Step{tableT()})
	want := []Step{
		AddColumn{Table: "t", Column: Column{Name: "d", Type: "text"}},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "d"}},
	}
	if !reflect.DeepEqual(m.Up, want) {
		t.Fatalf("Up = %#v, want %#v", m.Up, want)
	}
}

func TestDiffRenamesADerivedIndexAfterEveryRename(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	model := Model{Name: "u", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "x", Type: "integer"},
		{Name: "b", Type: "text"},
		{Name: "c", Type: "text"},
	}, Indexes: []CompositeIndex{{Columns: []string{"x", "b"}}}}
	m := compositeMigration(t, model,
		[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}}},
		Rename{App: "app", Table: "t", To: "u"},
		Rename{App: "app", Table: "t", Column: "a", To: "x"},
	)
	want := []Step{
		RenameTable{From: "t", To: "u"},
		RenameColumn{Table: "u", From: "a", To: "x"},
		RenameCompositeIndex{Table: "u", Columns: []string{"x", "b"}, From: "idx_t_a_b", To: "idx_u_x_b"},
	}
	if !reflect.DeepEqual(m.Up, want) {
		t.Fatalf("Up = %#v, want %#v", m.Up, want)
	}
	wantDown := []Step{
		RenameCompositeIndex{Table: "u", Columns: []string{"x", "b"}, From: "idx_u_x_b", To: "idx_t_a_b"},
		RenameColumn{Table: "u", From: "x", To: "a"},
		RenameTable{From: "u", To: "t"},
	}
	if !reflect.DeepEqual(m.Down, wantDown) {
		t.Fatalf("Down = %#v, want %#v", m.Down, wantDown)
	}
}

func TestDiffKeepsAnExplicitNameThroughRenames(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	model := Model{Name: "u", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "x", Type: "integer"},
		{Name: "b", Type: "text"},
		{Name: "c", Type: "text"},
	}, Indexes: []CompositeIndex{{Columns: []string{"x", "b"}, Name: "ix_pair"}}}
	m := compositeMigration(t, model,
		[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Name: "ix_pair"}},
		Rename{App: "app", Table: "t", To: "u"},
		Rename{App: "app", Table: "t", Column: "a", To: "x"},
	)
	want := []Step{RenameTable{From: "t", To: "u"}, RenameColumn{Table: "u", From: "a", To: "x"}}
	if !reflect.DeepEqual(m.Up, want) {
		t.Fatalf("Up = %#v, want %#v: an explicit name is never renamed by a table or column rename", m.Up, want)
	}
}

func TestDiffRenamesAnIndexWhoseDeclaredNameChanged(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	base := []Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}}}
	named := compositeMigration(t, modelT(CompositeIndex{Columns: []string{"a", "b"}, Name: "ix_pair"}), base)
	if want := []Step{RenameCompositeIndex{Table: "t", Columns: []string{"a", "b"}, From: "idx_t_a_b", To: "ix_pair"}}; !reflect.DeepEqual(named.Up, want) {
		t.Fatalf("Up = %#v, want %#v", named.Up, want)
	}
	back := compositeMigration(t, modelT(CompositeIndex{Columns: []string{"a", "b"}}),
		[]Step{tableT(), CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Name: "ix_pair"}})
	if want := []Step{RenameCompositeIndex{Table: "t", Columns: []string{"a", "b"}, From: "ix_pair", To: "idx_t_a_b"}}; !reflect.DeepEqual(back.Up, want) {
		t.Fatalf("Up = %#v, want %#v", back.Up, want)
	}
}

func TestDiffRefusesRenamesThatSwapIndexNames(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	state := replayUp(t, tableT(),
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}, Name: "ix_one"},
		CreateCompositeIndex{Table: "t", Columns: []string{"a", "c"}, Name: "ix_two"},
	)
	model := modelT(
		CompositeIndex{Columns: []string{"a", "b"}, Name: "ix_two"},
		CompositeIndex{Columns: []string{"a", "c"}, Name: "ix_one"},
	)
	model.App = "app"
	_, err := DiffModels([]Model{model}, state)
	if !errors.Is(err, ErrUnsupportedChange) || !strings.Contains(err.Error(), `"ix_two"`) {
		t.Fatalf("DiffModels error = %v, want ErrUnsupportedChange naming ix_two", err)
	}
}

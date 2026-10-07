package migration

import (
	"testing"
	"time"

	"github.com/angvp/tango/model"
)

type diffAllKinds struct {
	ID        int64 `tango:"pk"`
	Published bool
	Rating    float64
	PostedAt  time.Time
}

func TestSQLTypeCoversEveryFieldKind(t *testing.T) {
	registry := model.NewRegistry()
	registry.SetCurrentApp("content")
	if err := registry.Register(diffAllKinds{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, ok := registry.Get("diffAllKinds")
	if !ok {
		t.Fatal("diffAllKinds not registered")
	}

	byName := map[string]Column{}
	for _, c := range ModelsFromMeta([]model.ModelMeta{meta})[0].Columns {
		byName[c.Name] = c
	}

	cases := map[string]string{
		"id":        "integer",
		"published": "boolean",
		"rating":    "real",
		"posted_at": "timestamp",
	}
	for column, wantType := range cases {
		c, ok := byName[column]
		if !ok {
			t.Fatalf("column %q not found among %+v", column, byName)
		}
		if c.Type != wantType {
			t.Errorf("column %q Type = %q, want %q", column, c.Type, wantType)
		}
	}
}

func TestDesiredColumnSetsReferencesForForeignKeyFields(t *testing.T) {
	// diffPost has no foreign key field; register one that does.
	type diffPostWithAuthor struct {
		ID       int64 `tango:"pk"`
		AuthorID int64 `tango:"fk=diffUser"`
	}
	registry := model.NewRegistry()
	registry.SetCurrentApp("posts")
	if err := registry.Register(diffPostWithAuthor{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	withFK, ok := registry.Get("diffPostWithAuthor")
	if !ok {
		t.Fatal("diffPostWithAuthor not registered")
	}

	models := ModelsFromMeta([]model.ModelMeta{withFK})
	var authorIDColumn Column
	for _, c := range models[0].Columns {
		if c.Name == "author_id" {
			authorIDColumn = c
		}
	}
	if authorIDColumn.References != "diff_user" {
		t.Fatalf("author_id References = %q, want %q", authorIDColumn.References, "diff_user")
	}
}

func TestDiffColumnToggledUniqueFalseToTrueProducesAlterColumnUnique(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "users", Up: []Step{
			CreateTable{Table: "diff_user", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "email", Type: "text", Unique: false},
			}},
		}},
	})

	meta := registerDiffModel(t, "users", diffUser{}) // diffUser's Email is tango:"unique"
	migrations := mustDiff(t, []model.ModelMeta{meta}, state)
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]

	var up, down *AlterColumnUnique
	for i := range m.Up {
		if s, ok := m.Up[i].(AlterColumnUnique); ok {
			up = &s
		}
	}
	for i := range m.Down {
		if s, ok := m.Down[i].(AlterColumnUnique); ok {
			down = &s
		}
	}
	if up == nil || !up.Unique {
		t.Fatalf("Up steps = %+v, want an AlterColumnUnique{Unique: true}", m.Up)
	}
	if down == nil || down.Unique {
		t.Fatalf("Down steps = %+v, want an AlterColumnUnique{Unique: false} to reverse it", m.Down)
	}
}

func TestDiffColumnToggledIndexedTrueToFalseProducesDropIndex(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "posts", Up: []Step{
			CreateTable{Table: "diff_post", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "category", Type: "text", Indexed: true},
			}},
		}},
	})

	// diffPost's Category is tango:"index", so nothing changes here; use a
	// hand-built desired column set with Indexed: false instead, via
	// DiffModels directly, to force the "no longer indexed" branch.
	desired := []Model{{App: "posts", Name: "diff_post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "category", Type: "text", Indexed: false},
	}}}

	migrations := mustDiffModels(t, desired, state)
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]

	var up, down bool
	for _, s := range m.Up {
		if _, ok := s.(DropIndex); ok {
			up = true
		}
	}
	for _, s := range m.Down {
		if _, ok := s.(CreateIndex); ok {
			down = true
		}
	}
	if !up {
		t.Fatalf("Up steps = %+v, want a DropIndex", m.Up)
	}
	if !down {
		t.Fatalf("Down steps = %+v, want a CreateIndex to reverse it", m.Down)
	}
	if !m.Reversible {
		t.Fatal("Reversible = false, want true for an index toggle")
	}
}

// referenceOrderModels has a table referencing a later one (alpha ->
// gamma), a self-reference (beta), and a reference cycle (pi <-> rho),
// which no order creates referenced-first.
func referenceOrderModels() []Model {
	id := Column{Name: "id", Type: "integer", PrimaryKey: true}
	ref := func(table string) Column { return Column{Name: table + "_id", Type: "integer", References: table} }
	return []Model{
		{App: "blog", Name: "alpha", Columns: []Column{id, ref("gamma")}},
		{App: "blog", Name: "beta", Columns: []Column{id, ref("beta")}},
		{App: "blog", Name: "gamma", Columns: []Column{id}},
		{App: "blog", Name: "pi", Columns: []Column{id, ref("rho")}},
		{App: "blog", Name: "rho", Columns: []Column{id, ref("pi")}},
	}
}

func stepTables(t *testing.T, steps []Step) []string {
	t.Helper()
	var tables []string
	for _, step := range steps {
		switch s := step.(type) {
		case CreateTable:
			tables = append(tables, s.Table)
		case DropTable:
			tables = append(tables, s.Table)
		default:
			t.Fatalf("unexpected step %#v", step)
		}
	}
	return tables
}

func assertEachTableOnceWithBefore(t *testing.T, tables []string, first, second string) {
	t.Helper()
	position := make(map[string]int)
	for i, table := range tables {
		if _, seen := position[table]; seen {
			t.Fatalf("table %q appears twice in %v", table, tables)
		}
		position[table] = i
	}
	for _, table := range []string{"alpha", "beta", "gamma", "pi", "rho"} {
		if _, ok := position[table]; !ok {
			t.Fatalf("table %q missing from %v", table, tables)
		}
	}
	if position[first] > position[second] {
		t.Fatalf("%q comes after %q in %v", first, second, tables)
	}
}

func TestDiffCreatesReferencedTablesFirstAndToleratesSelfReferencesAndCycles(t *testing.T) {
	migrations := mustDiffModels(t, referenceOrderModels(), SchemaState{Tables: map[string]TableState{}})
	if len(migrations) != 1 {
		t.Fatalf("DiffModels returned %d migrations, want 1", len(migrations))
	}
	assertEachTableOnceWithBefore(t, stepTables(t, migrations[0].Up), "gamma", "alpha")
}

func TestDiffDropsReferencingTablesFirstAndToleratesSelfReferencesAndCycles(t *testing.T) {
	state, err := Replay([]Migration{{App: "blog", Up: []Step{
		CreateTable{Table: "gamma", Columns: referenceOrderModels()[2].Columns},
		CreateTable{Table: "alpha", Columns: referenceOrderModels()[0].Columns},
		CreateTable{Table: "beta", Columns: referenceOrderModels()[1].Columns},
		CreateTable{Table: "pi", Columns: referenceOrderModels()[3].Columns},
		CreateTable{Table: "rho", Columns: referenceOrderModels()[4].Columns},
	}}})
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}
	migrations := mustDiffModels(t, nil, state)
	if len(migrations) != 1 {
		t.Fatalf("DiffModels returned %d migrations, want 1", len(migrations))
	}
	assertEachTableOnceWithBefore(t, stepTables(t, migrations[0].Up), "alpha", "gamma")
}

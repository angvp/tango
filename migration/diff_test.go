package migration

import (
	"testing"

	"github.com/angvp/tango/model"
)

type diffUser struct {
	ID    int64  `tango:"pk"`
	Email string `tango:"unique"`
}

type diffPost struct {
	ID       int64  `tango:"pk"`
	Category string `tango:"index"`
}

func registerDiffModel(t *testing.T, app string, value any) model.ModelMeta {
	t.Helper()
	registry := model.NewRegistry()
	registry.SetCurrentApp(app)
	if err := registry.Register(value); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	name := modelName(value)
	meta, ok := registry.Get(name)
	if !ok {
		t.Fatalf("model %q not registered", name)
	}
	return meta
}

func modelName(value any) string {
	switch value.(type) {
	case diffUser:
		return "diffUser"
	case diffPost:
		return "diffPost"
	default:
		panic("unknown test model")
	}
}

func TestDiffNewModelProducesCreateTable(t *testing.T) {
	meta := registerDiffModel(t, "users", diffUser{})

	migrations := Diff([]model.ModelMeta{meta}, SchemaState{Tables: map[string]TableState{}})
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]
	if m.App != "users" {
		t.Fatalf("App = %q, want %q", m.App, "users")
	}
	if len(m.Up) != 1 {
		t.Fatalf("Up has %d steps, want 1", len(m.Up))
	}
	create, ok := m.Up[0].(CreateTable)
	if !ok {
		t.Fatalf("step type = %T, want CreateTable", m.Up[0])
	}
	if create.Table != "diff_user" {
		t.Fatalf("Table = %q, want %q", create.Table, "diff_user")
	}
	if !m.Reversible {
		t.Fatalf("Reversible = false, want true for a new table")
	}
	if len(m.Down) != 1 {
		t.Fatalf("Down has %d steps, want 1", len(m.Down))
	}
	if _, ok := m.Down[0].(DropTable); !ok {
		t.Fatalf("Down step type = %T, want DropTable", m.Down[0])
	}
}

func TestDiffRemovedModelProducesIrreversibleDropTable(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "users", Up: []Step{
			CreateTable{Table: "diff_user", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}},
	})

	migrations := Diff(nil, state)
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]
	if m.Reversible {
		t.Fatalf("Reversible = true, want false for a dropped table")
	}
	if _, ok := m.Up[0].(DropTable); !ok {
		t.Fatalf("step type = %T, want DropTable", m.Up[0])
	}
}

func TestDiffAddedFieldProducesAddColumn(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "users", Up: []Step{
			CreateTable{Table: "diff_user", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		}},
	})

	meta := registerDiffModel(t, "users", diffUser{})
	migrations := Diff([]model.ModelMeta{meta}, state)
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]
	if len(m.Up) != 1 {
		t.Fatalf("Up has %d steps, want 1", len(m.Up))
	}
	add, ok := m.Up[0].(AddColumn)
	if !ok {
		t.Fatalf("step type = %T, want AddColumn", m.Up[0])
	}
	if add.Column.Name != "email" {
		t.Fatalf("Column.Name = %q, want %q", add.Column.Name, "email")
	}
	if !m.Reversible {
		t.Fatalf("Reversible = false, want true for an added column")
	}
}

func TestDiffRemovedFieldProducesIrreversibleDropColumn(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "users", Up: []Step{
			CreateTable{Table: "diff_user", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "legacy", Type: "text"},
			}},
		}},
	})

	meta := registerDiffModel(t, "users", diffUser{})
	migrations := Diff([]model.ModelMeta{meta}, state)
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]
	if m.Reversible {
		t.Fatalf("Reversible = true, want false for a dropped column")
	}

	found := false
	for _, step := range m.Up {
		if drop, ok := step.(DropColumn); ok && drop.Column == "legacy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a DropColumn for %q, got %+v", "legacy", m.Up)
	}
}

func TestDiffToggledUniqueAndIndexedProducesAlterSteps(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "posts", Up: []Step{
			CreateTable{Table: "diff_post", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "category", Type: "text"},
			}},
		}},
	})

	meta := registerDiffModel(t, "posts", diffPost{})
	migrations := Diff([]model.ModelMeta{meta}, state)
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	m := migrations[0]
	if !m.Reversible {
		t.Fatalf("Reversible = false, want true for an index toggle")
	}

	found := false
	for _, step := range m.Up {
		if _, ok := step.(CreateIndex); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a CreateIndex step, got %+v", m.Up)
	}
}

func TestDiffNoChangesProducesNoMigrations(t *testing.T) {
	state, _ := Replay([]Migration{
		{Name: "0001", App: "users", Up: []Step{
			CreateTable{Table: "diff_user", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "email", Type: "text", Unique: true},
			}},
		}},
	})

	meta := registerDiffModel(t, "users", diffUser{})
	migrations := Diff([]model.ModelMeta{meta}, state)
	if len(migrations) != 0 {
		t.Fatalf("got %d migrations, want 0", len(migrations))
	}
}

func TestDiffSpansMultipleAppsIndependently(t *testing.T) {
	userMeta := registerDiffModel(t, "users", diffUser{})
	postMeta := registerDiffModel(t, "posts", diffPost{})

	migrations := Diff([]model.ModelMeta{userMeta, postMeta}, SchemaState{Tables: map[string]TableState{}})
	if len(migrations) != 2 {
		t.Fatalf("got %d migrations, want 2", len(migrations))
	}

	apps := map[string]bool{}
	for _, m := range migrations {
		apps[m.App] = true
	}
	if !apps["users"] || !apps["posts"] {
		t.Fatalf("apps present = %+v, want both users and posts", apps)
	}
}

package migration

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// usersState is the replayed state of the exact-schema Users model.
func usersState(t *testing.T) SchemaState {
	t.Helper()
	first, err := Diff([]model.ModelMeta{exactMeta()}, SchemaState{Tables: map[string]TableState{}})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	state, err := Replay(first)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return state
}

func diffChanged(t *testing.T, change func(*model.ModelMeta)) ([]Migration, SchemaState, error) {
	t.Helper()
	state := usersState(t)
	meta := exactMeta()
	change(&meta)
	got, err := Diff([]model.ModelMeta{meta}, state)
	return got, state, err
}

func TestDiffEmitsStepsForExactSchemaChanges(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	got, state, err := diffChanged(t, func(m *model.ModelMeta) {
		m.Fields[3].DefaultValue = "es"
		m.Fields[4].NotNull = false
		m.Fields[1].UniqueName = "uniq_email"
		m.Fields[2].IndexName = "idx_slug_new"
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("Diff = %+v, %v", got, err)
	}
	if !got[0].Reversible {
		t.Error("migration is irreversible, want reversible")
	}
	wantUp := []Step{
		RenameIndex{Table: "users", Column: "email", Unique: true, From: "idx_users_email", To: "uniq_email"},
		RenameIndex{Table: "users", Column: "slug", Unique: false, From: "idx_users_slug", To: "idx_slug_new"},
		AlterColumnDefault{Table: "users", Column: "language", DefaultKind: "string", DefaultValue: "es"},
		AlterColumnNotNull{Table: "users", Column: "created_at", NotNull: false},
	}
	if !reflect.DeepEqual(got[0].Up, wantUp) {
		t.Fatalf("Up =\n%#v\nwant\n%#v", got[0].Up, wantUp)
	}
	wantDown := []Step{
		AlterColumnNotNull{Table: "users", Column: "created_at", NotNull: true},
		AlterColumnDefault{Table: "users", Column: "language", DefaultKind: "string", DefaultValue: "en"},
		RenameIndex{Table: "users", Column: "slug", Unique: false, From: "idx_slug_new", To: "idx_users_slug"},
		RenameIndex{Table: "users", Column: "email", Unique: true, From: "uniq_email", To: "idx_users_email"},
	}
	if !reflect.DeepEqual(got[0].Down, wantDown) {
		t.Fatalf("Down =\n%#v\nwant\n%#v", got[0].Down, wantDown)
	}

	next, err := Replay(append(replayInputs(t), got...))
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if reflect.DeepEqual(next, state) {
		t.Fatal("replaying the steps left the state unchanged")
	}
}

func replayInputs(t *testing.T) []Migration {
	t.Helper()
	first, err := Diff([]model.ModelMeta{exactMeta()}, SchemaState{Tables: map[string]TableState{}})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return first
}

func TestDiffExactStepsReplayToACleanSecondDiff(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	changes := map[string]func(*model.ModelMeta){
		"default":      func(m *model.ModelMeta) { m.Fields[3].DefaultKind, m.Fields[3].DefaultValue = "", "" },
		"not null":     func(m *model.ModelMeta) { m.Fields[4].NotNull = false },
		"unique name":  func(m *model.ModelMeta) { m.Fields[1].UniqueName = "uniq_email" },
		"index gained": func(m *model.ModelMeta) { m.Fields[3].Indexed, m.Fields[3].IndexName = true, "idx_lang" },
		"index lost":   func(m *model.ModelMeta) { m.Fields[2].Indexed, m.Fields[2].IndexName = false, "" },
		"naive to tz":  func(m *model.ModelMeta) { m.Fields[4].Naive = false },
		"int4 to int8": func(m *model.ModelMeta) { m.Fields[0].Int4 = false },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			history := replayInputs(t)
			meta := exactMeta()
			change(&meta)
			got, err := Diff([]model.ModelMeta{meta}, replayOrFatal(t, history))
			if err != nil || len(got) != 1 {
				t.Fatalf("Diff = %+v, %v", got, err)
			}
			state := replayOrFatal(t, append(history, got...))
			again, err := Diff([]model.ModelMeta{meta}, state)
			if err != nil || len(again) != 0 {
				t.Fatalf("second Diff = %+v, %v; want no migration", again, err)
			}
		})
	}
}

func replayOrFatal(t *testing.T, migrations []Migration) SchemaState {
	t.Helper()
	state, err := Replay(migrations)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return state
}

func TestDiffWideningIsIrreversible(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	got, _, err := diffChanged(t, func(m *model.ModelMeta) { m.Fields[0].Int4 = false; m.Fields[4].Naive = false })
	if err != nil || len(got) != 1 {
		t.Fatalf("Diff = %+v, %v", got, err)
	}
	if got[0].Reversible {
		t.Error("an int4 and naive widening is reversible, want irreversible")
	}
	wantUp := []Step{
		AlterColumnType{Table: "users", Column: "id", From: "integer", To: "integer", FromWidth: 32},
		AlterColumnType{Table: "users", Column: "created_at", From: "timestamp", To: "timestamp", FromNaive: true},
	}
	if !reflect.DeepEqual(got[0].Up, wantUp) {
		t.Fatalf("Up =\n%#v\nwant\n%#v", got[0].Up, wantUp)
	}
}

func TestDiffRefusesWhatNoStepExpresses(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	meta := func(change func(*model.ModelMeta)) error {
		_, _, err := diffChanged(t, change)
		return err
	}
	refused := func(name string, err error, want string) {
		t.Helper()
		if !errors.Is(err, ErrUnsupportedChange) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want ErrUnsupportedChange containing %q", name, err, want)
		}
	}
	// A new NOT NULL column with no default cannot fill existing rows.
	refused("new not null", func() error {
		state := usersState(t)
		m := exactMeta()
		m.Fields = append(m.Fields, model.FieldMeta{Name: "Nick", Type: m.Fields[1].Type, NotNull: true, Editable: true})
		_, err := Diff([]model.ModelMeta{m}, state)
		return err
	}(), "NOT NULL")
	// With a default the column is accepted.
	state := usersState(t)
	m := exactMeta()
	m.Fields = append(m.Fields, model.FieldMeta{Name: "Nick", Type: m.Fields[1].Type, NotNull: true, DefaultKind: "string", DefaultValue: "x", Editable: true})
	if _, err := Diff([]model.ModelMeta{m}, state); err != nil {
		t.Errorf("new NOT NULL column with a default refused: %v", err)
	}
	// A typed default whose kind does not fit the new type.
	{
		state := replayOrFatal(t, []Migration{{App: "blog", Name: "0001", Up: []Step{CreateTable{Table: "flags", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "on", Type: "boolean", DefaultKind: "boolean", DefaultValue: "true"},
		}}}}})
		models := []Model{{App: "blog", Name: "flags", Struct: "Flags", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "on", Type: "integer"},
		}}}
		_, err := DiffModels(models, state)
		refused("default kind", err, "its default kind")
	}
	// The primary key may only widen from int4.
	refused("pk type", meta(func(m *model.ModelMeta) { m.Fields[0].Type = m.Fields[1].Type; m.Fields[0].Int4 = false }), "a primary key's type cannot change")
}

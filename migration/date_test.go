package migration

import (
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

type dateRecord struct {
	ID  int64 `tango:"pk"`
	Day model.Date
}

// TestDiffMapsADateFieldToADateColumn pins the explicit token. Without it a
// model.Date would fall through to integer and create the wrong column.
func TestDiffMapsADateFieldToADateColumn(t *testing.T) {
	registry := model.NewRegistry()
	if err := registry.Register(dateRecord{}); err != nil {
		t.Fatal(err)
	}
	migrations, err := Diff(registry.All(), SchemaState{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	var found bool
	for _, m := range migrations {
		for _, step := range m.Up {
			create, ok := step.(CreateTable)
			if !ok {
				continue
			}
			for _, column := range create.Columns {
				if column.Name == "day" {
					found = true
					if column.Type != "date" {
						t.Errorf("day column type = %q, want date", column.Type)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("no day column in the generated CreateTable")
	}
}

func TestDateTypeIsDATEOnEveryDialect(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		if got := baseTypeSQL(dialect, "date", 0); got != "DATE" {
			t.Errorf("dialect %d: date renders as %q, want DATE", dialect, got)
		}
	}
}

func TestDateIsNotUnboundedTextOnMySQL(t *testing.T) {
	if mysqlUnboundedText("date") {
		t.Error("date is treated as unbounded text on MySQL, which would render LONGTEXT")
	}
}

// TestAddingADateColumnSuppliesNoDefault: existing rows get NULL, which reads
// as the zero date, so the step carries no default and no NOT NULL.
func TestAddingADateColumnSuppliesNoDefault(t *testing.T) {
	state := SchemaState{Tables: map[string]TableState{
		"sample": {Name: "sample", App: "shop", Columns: []ColumnState{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}
	models := []Model{{
		App: "shop", Name: "sample", Struct: "Sample",
		Fields:  map[string]string{"id": "ID", "day": "Day"},
		Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "day", Type: "date"}},
	}}
	migrations, err := DiffModels(models, state)
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	want := []Migration{{
		App:        "shop",
		Up:         []Step{AddColumn{Table: "sample", Column: Column{Name: "day", Type: "date"}}},
		Down:       []Step{DropColumn{Table: "sample", Column: "day"}},
		Reversible: true,
	}}
	if !reflect.DeepEqual(migrations, want) {
		t.Errorf("migrations = %#v, want %#v", migrations, want)
	}
}

// TestDateTypeChangesAreRefused: no Widening or narrowing involves date, so
// makemigrations refuses every change in either direction, as it does for
// timestamp.
func TestDateTypeChangesAreRefused(t *testing.T) {
	for _, change := range []struct{ from, to string }{
		{"date", "timestamp"}, {"timestamp", "date"}, {"date", "text"}, {"text", "date"}, {"date", "integer"},
	} {
		migrations, err := DiffModels(sampleModels(change.to, "v"), sampleState(change.from, ""))
		if !errors.Is(err, ErrUnsupportedChange) || migrations != nil {
			t.Errorf("%s to %s: err = %v, want ErrUnsupportedChange", change.from, change.to, err)
		}
	}
}

package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

// TestDateColumnTokenSurvivesMigrationJSON writes a migration that creates a
// date column and adds one, then reads it back. The token must be exactly
// "date" in the document and in the decoded steps.
func TestDateColumnTokenSurvivesMigrationJSON(t *testing.T) {
	want := []migration.Migration{{
		App:  "shop",
		Name: "0001_dates",
		Up: []migration.Step{
			migration.CreateTable{Table: "event", Columns: []migration.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "day", Type: "date"},
			}},
			migration.AddColumn{Table: "event", Column: migration.Column{Name: "held_on", Type: "date"}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "event", Column: "held_on"},
			migration.DropTable{Table: "event"},
		},
		Reversible: true,
	}}
	encoded, err := encodeMigrations(want)
	if err != nil {
		t.Fatalf("encodeMigrations: %v", err)
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Type":"date"`) {
		t.Errorf("the document does not carry the date token: %s", data)
	}
	got, err := decodeMigrations(data)
	if err != nil {
		t.Fatalf("decodeMigrations: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the migration:\n got %#v\nwant %#v", got, want)
	}
}

package cli

import (
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

// shopModels is a "shop" app with a Widget and a Gadget model; drop removes
// models or columns from it.
func shopModels(dropStock, dropGadget bool) []migration.Model {
	widget := widgetModel(func([]migration.Column) {})
	if dropStock {
		widget[0].Columns = widget[0].Columns[:1]
	}
	models := widget
	if !dropGadget {
		models = append(models, migration.Model{
			App: "shop", Name: "gadget", Struct: "Gadget", Fields: map[string]string{"id": "ID"},
			Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}},
		})
	}
	return models
}

// runMakeMigrations runs makemigrations with args against models and
// returns its exit code and stderr.
func runMakeMigrations(t *testing.T, dir string, models []migration.Model, args ...string) (int, string) {
	t.Helper()
	var stderr strings.Builder
	code := Run(context.Background(), append([]string{"makemigrations"}, args...), dir, io.Discard, &stderr, dumpModelsRunner{models: models})
	return code, stderr.String()
}

// assertRefusedAndUnchanged checks a run failed, said each of wants, and
// left the migrations directory exactly as before.
func assertRefusedAndUnchanged(t *testing.T, dir string, before map[string]string, code int, stderr string, wants ...string) {
	t.Helper()
	if code == 0 {
		t.Fatal("makemigrations exit code = 0, want non-zero")
	}
	for _, want := range wants {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr %q does not mention %q", stderr, want)
		}
	}
	if after := migrationsDirContents(t, dir); !maps.Equal(after, before) {
		t.Fatalf("migrations dir changed after a refused run:\nbefore %v\nafter  %v", before, after)
	}
}

func TestMakeMigrationsRefusesAnUnauthorisedDrop(t *testing.T) {
	tests := []struct {
		name                  string
		dropStock, dropGadget bool
		wants                 []string
	}{
		{"field", true, false, []string{"shop.widget.stock", "--allow-drop shop.widget.stock", "--rename"}},
		{"model", false, true, []string{"shop.gadget", "--allow-drop shop.gadget"}},
		{"field and model", true, true, []string{"--allow-drop shop.widget.stock", "--allow-drop shop.gadget"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeInitialMigration(t, dir, shopModels(false, false))
			before := migrationsDirContents(t, dir)

			code, stderr := runMakeMigrations(t, dir, shopModels(tt.dropStock, tt.dropGadget))

			assertRefusedAndUnchanged(t, dir, before, code, stderr, tt.wants...)
		})
	}
}

func TestMakeMigrationsDropsWhatEachAllowDropNames(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, shopModels(false, false))

	code, stderr := runMakeMigrations(t, dir, shopModels(true, true), "--allow-drop", "shop.Widget.Stock", "--allow-drop", "shop.Gadget")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr)
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "0002_*.go"))
	if err != nil || len(files) != 1 {
		t.Fatalf("second migration files = %v (%v), want one", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for _, want := range []string{"migration.DropColumn{Table: \"widget\", Column: \"stock\"}", "migration.DropTable{Table: \"gadget\"}", "Reversible: false"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("migration does not contain %q:\n%s", want, content)
		}
	}
}

func TestMakeMigrationsRejectsAllowDropsThatDoNotMatchOneDrop(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		wants []string
	}{
		{"names something not dropped", []string{"--allow-drop", "shop.Widget.Stock", "--allow-drop", "shop.Widget.ID"}, []string{"shop.Widget.ID", "not dropped"}},
		{"names a model that is not dropped", []string{"--allow-drop", "shop.Widget.Stock", "--allow-drop", "shop.Gadget"}, []string{"shop.Gadget", "not dropped"}},
		{"authorises the same item twice", []string{"--allow-drop", "shop.Widget.Stock", "--allow-drop", "shop.widget.stock"}, []string{"shop.widget.stock", "more than once"}},
		{"is malformed", []string{"--allow-drop", "shop.Widget.Stock", "--allow-drop", "Stock"}, []string{"Stock", "app.Model.Field", "app.Model"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeInitialMigration(t, dir, shopModels(false, false))
			before := migrationsDirContents(t, dir)

			code, stderr := runMakeMigrations(t, dir, shopModels(true, false), tt.args...)

			assertRefusedAndUnchanged(t, dir, before, code, stderr, tt.wants...)
		})
	}
}

func TestUsageDocumentsAllowDrop(t *testing.T) {
	var stdout strings.Builder
	Run(context.Background(), []string{"help"}, t.TempDir(), &stdout, io.Discard, dumpModelsRunner{})
	if !strings.Contains(stdout.String(), "--allow-drop <app.Model[.Field]>") {
		t.Fatalf("usage does not document --allow-drop:\n%s", stdout.String())
	}
}

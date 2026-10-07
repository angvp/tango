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

// migrationsDirContents maps each file in dir's migrations directory to its
// content, so a test can check a failed run wrote nothing.
func migrationsDirContents(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "migrations"))
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	contents := make(map[string]string, len(entries))
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, "migrations", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		contents[entry.Name()] = string(content)
	}
	return contents
}

// widgetModel is a "shop" app's Widget model, with change applied to its
// columns.
func widgetModel(change func(columns []migration.Column)) []migration.Model {
	columns := []migration.Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "stock", Type: "integer"},
	}
	change(columns)
	return []migration.Model{{
		App: "shop", Name: "widget", Columns: columns,
		Struct: "Widget", Fields: map[string]string{"id": "ID", "stock": "Stock"},
	}}
}

// makeInitialMigration runs makemigrations once for models, so a later run
// diffs against real history.
func makeInitialMigration(t *testing.T, dir string, models []migration.Model) {
	t.Helper()
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, dumpModelsRunner{models: models}); code != 0 {
		t.Fatalf("initial makemigrations exit code = %d, want 0", code)
	}
}

func TestMakeMigrationsRefusesATypeChangeAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, widgetModel(func([]migration.Column) {}))
	before := migrationsDirContents(t, dir)

	var stderr strings.Builder
	runner := dumpModelsRunner{models: widgetModel(func(c []migration.Column) { c[1].Type = "text" })}
	code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, &stderr, runner)

	if code == 0 {
		t.Fatal("makemigrations exit code = 0 for a type change, want non-zero")
	}
	for _, want := range []string{"shop.Widget.Stock", "integer", "text"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr %q does not mention %q", stderr.String(), want)
		}
	}
	if after := migrationsDirContents(t, dir); !maps.Equal(after, before) {
		t.Fatalf("migrations dir changed after a refused run:\nbefore %v\nafter  %v", before, after)
	}
}

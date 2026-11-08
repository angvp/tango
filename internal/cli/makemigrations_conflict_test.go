package cli

import (
	"context"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

// plantMigration writes one extra migration file next to the generated ones,
// as a merged branch would leave it.
func plantMigration(t *testing.T, dir string, filename string, m migration.Migration) {
	t.Helper()
	path := filepath.Join(dir, "migrations", filename)
	if err := writeMigrationFile(path, migrationVarName(m.Name), []migration.Migration{m}); err != nil {
		t.Fatalf("planting %s: %v", filename, err)
	}
}

// planted has no steps: these fixtures exist only for their identity, and a
// table they created would be a model the registry does not declare, which
// the drop guard refuses before the conflict check is reached.
func planted(app string, name string) migration.Migration {
	return migration.Migration{App: app, Name: name, Reversible: true}
}

// widgetWithNote adds an optional text column to the shop Widget model. The
// change is additive, so an unguarded run would generate a new migration file.
func widgetWithNote() []migration.Model {
	return []migration.Model{{
		App: "shop", Name: "widget", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "stock", Type: "integer"},
			{Name: "note", Type: "text"},
		},
		Struct: "Widget", Fields: map[string]string{"id": "ID", "stock": "Stock", "note": "Note"},
	}}
}

// loadedNames returns the migration names on disk, failing if there are none,
// so a test can reference the name the generator actually chose.
func loadedNames(t *testing.T, dir string) []string {
	t.Helper()
	existing, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(existing) == 0 {
		t.Fatal("no migrations on disk, want the generated one")
	}
	names := make([]string, len(existing))
	for i, m := range existing {
		names[i] = m.Name
	}
	return names
}

// runMakeMigrationsCapturing runs makemigrations and returns its exit code and
// stderr, so each test asserts on the refusal text as well as the exit code.
func runMakeMigrationsCapturing(t *testing.T, dir string, models []migration.Model) (int, string) {
	t.Helper()
	var stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, &stderr, dumpModelsRunner{models: models})
	return code, stderr.String()
}

func TestMakeMigrationsRefusesSharedNumberAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, widgetModel(func([]migration.Column) {}))
	generated := loadedNames(t, dir)[0]
	plantMigration(t, dir, "branch_phone.go", planted("shop", "0001_phone"))
	before := migrationsDirContents(t, dir)

	code, stderr := runMakeMigrationsCapturing(t, dir, widgetWithNote())

	assertRefusedWithoutWriting(t, dir, before, code, stderr)
	want := `number 0001 is shared by "` + generated + `", "0001_phone"`
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr %q does not contain %q", stderr, want)
	}
}

func TestMakeMigrationsRefusesExactDuplicateNameInOneApp(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, widgetModel(func([]migration.Column) {}))
	plantMigration(t, dir, "branch_copy.go", planted("shop", loadedNames(t, dir)[0]))
	before := migrationsDirContents(t, dir)

	code, stderr := runMakeMigrationsCapturing(t, dir, widgetWithNote())

	assertRefusedWithoutWriting(t, dir, before, code, stderr)
	if !strings.Contains(stderr, "is registered 2 times") {
		t.Fatalf("stderr %q does not report the exact duplicate", stderr)
	}
}

func TestMakeMigrationsAllowsSameNumberInDifferentApps(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, widgetModel(func([]migration.Column) {}))
	plantMigration(t, dir, "blog_first.go", planted("blog", "0001_first"))

	code, stderr := runMakeMigrationsCapturing(t, dir, widgetWithNote())

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for the same number in two apps; stderr:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "conflicting migration names") {
		t.Fatalf("stderr %q refuses a same-number pair across apps", stderr)
	}
}

// assertRefusedWithoutWriting checks the refusal shape shared by every
// conflict case: exit 1, the tango makemigrations prefix, and a migrations
// directory byte-identical to its state before the run.
func assertRefusedWithoutWriting(t *testing.T, dir string, before map[string]string, code int, stderr string) {
	t.Helper()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "tango makemigrations: ") || !strings.Contains(stderr, "conflicting migration names") {
		t.Fatalf("stderr %q does not carry the conflict refusal", stderr)
	}
	if after := migrationsDirContents(t, dir); !maps.Equal(after, before) {
		t.Fatalf("migrations dir changed after a refused run:\nbefore %v\nafter  %v", before, after)
	}
}

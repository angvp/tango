package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// syncAggregate regenerates migrations.go from the files on disk, as a
// previous makemigrations run would have left it. Without this the aggregate
// lags the planted files, and any rewrite of it changes content rather than
// only its metadata.
func syncAggregate(t *testing.T, dir string) {
	t.Helper()
	if err := regenerateMigrationsAggregate(dir); err != nil {
		t.Fatalf("syncing aggregate: %v", err)
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
	syncAggregate(t, dir)
	setOldMtimes(t, dir)
	before := snapshotTree(t, dir)

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
	syncAggregate(t, dir)
	setOldMtimes(t, dir)
	before := snapshotTree(t, dir)

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

// treeEntry is everything observable about one path in the project tree. A
// rewrite with identical bytes still moves Mod, and a create-then-remove
// still moves the parent directory's Mod, so both show up here.
type treeEntry struct {
	Dir      bool
	Mode     fs.FileMode
	ModNs    int64
	Size     int64
	Digest   string
	Children string
}

// oldMtime is the fixed timestamp fixtures are reset to, so any write during
// the run leaves a modification time that cannot be mistaken for the setup's.
var oldMtime = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// setOldMtimes resets every entry under root, directories included, to
// oldMtime. Call it after the fixture is fully built.
func setOldMtimes(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, oldMtime, oldMtime)
	})
	if err != nil {
		t.Fatalf("resetting mtimes: %v", err)
	}
}

// snapshotTree records every entry under root, keyed by slash-relative path.
// Symlinks are recorded by target, not followed.
func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := map[string]treeEntry{}
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		entry := treeEntry{Mode: info.Mode(), ModNs: info.ModTime().UnixNano(), Size: info.Size()}
		switch {
		case info.IsDir():
			entry.Dir = true
			names, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			entry.Children = joinNames(names)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.Digest = "symlink:" + target
		default:
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(content)
			entry.Digest = hex.EncodeToString(sum[:])
		}
		snapshot[filepath.ToSlash(rel)] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	return snapshot
}

func joinNames(entries []fs.DirEntry) string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return strings.Join(names, "\n")
}

// changedPaths lists every path whose entry differs between before and after,
// including paths present in only one of them.
func changedPaths(before, after map[string]treeEntry) []string {
	var changed []string
	for path, entry := range before {
		if other, ok := after[path]; !ok || other != entry {
			changed = append(changed, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, path)
		}
	}
	return changed
}

// assertRefusedWithoutWriting checks the refusal shape shared by every
// conflict case: exit 1, the tango makemigrations prefix, and a project tree
// whose snapshot, including mtimes and directory listings, is identical to
// its state before the run.
func assertRefusedWithoutWriting(t *testing.T, dir string, before map[string]treeEntry, code int, stderr string) {
	t.Helper()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "tango makemigrations: ") || !strings.Contains(stderr, "conflicting migration names") {
		t.Fatalf("stderr %q does not carry the conflict refusal", stderr)
	}
	if changed := changedPaths(before, snapshotTree(t, dir)); len(changed) > 0 {
		t.Fatalf("project tree changed after a refused run: %v", changed)
	}
}

// TestMakeMigrationsSnapshotDetectsWrites is the positive control: with a
// valid registry the same snapshot comparison must see makemigrations write,
// or the refusal tests above prove nothing about the instrument.
func TestMakeMigrationsSnapshotDetectsWrites(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, widgetModel(func([]migration.Column) {}))
	setOldMtimes(t, dir)
	before := snapshotTree(t, dir)

	code, stderr := runMakeMigrationsCapturing(t, dir, widgetWithNote())

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid registry; stderr:\n%s", code, stderr)
	}
	changed := changedPaths(before, snapshotTree(t, dir))
	if !contains(changed, "migrations/migrations.go") {
		t.Fatalf("snapshot did not see the aggregate rewrite; changed = %v", changed)
	}
	created := false
	for _, path := range changed {
		if _, ok := before[path]; !ok && strings.HasPrefix(path, "migrations/") {
			created = true
		}
	}
	if !created {
		t.Fatalf("snapshot did not see a new migration file; changed = %v", changed)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

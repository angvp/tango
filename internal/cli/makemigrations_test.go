package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/migration"
)

type dumpModelsRunner struct {
	models []migration.Model
}

func (r dumpModelsRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	encoded, err := json.Marshal(r.models)
	if err != nil {
		return err
	}
	_, err = stdout.Write(encoded)
	return err
}

func TestMakeMigrationsCreatesFirstMigrationFile(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text", Unique: true},
		}},
	}}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d migration files, want 1", len(files))
	}
	if !regexp.MustCompile(`^0001_auto_\d{14}\.go$`).MatchString(filepath.Base(files[0])) {
		t.Fatalf("filename = %q, want 0001_auto_<UTC timestamp>.go", filepath.Base(files[0]))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if !strings.Contains(string(content), "CreateTable") {
		t.Fatalf("migration file does not contain CreateTable step:\n%s", content)
	}

	aggregate, err := os.ReadFile(filepath.Join(dir, "migrations", "migrations.go"))
	if err != nil {
		t.Fatalf("read migrations.go: %v", err)
	}
	if !strings.Contains(string(aggregate), "var Migrations = []migration.Migration{") || !strings.Contains(string(aggregate), "CreateTable") {
		t.Fatalf("migrations.go does not aggregate the generated migration:\n%s", aggregate)
	}
}

func TestMakeMigrationsGeneratedFileIncludesForeignKeyReference(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{
		{App: "authors", Name: "author", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
		{App: "posts", Name: "post", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "author_id", Type: "integer", References: "author"},
		}},
	}}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	var found bool
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(content), `References: "author"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no generated migration file's Go literal contains References: \"author\" — the foreign key constraint would silently not be created when applied")
	}
}

// TestMakeMigrationsGeneratedFileOmitsRedundantColumnType guards against
// simplifycompositelit warnings: elements of an already-typed
// []migration.Column slice must not repeat "migration.Column" (gopls flags
// this as "redundant type from array, slice, or map composite literal").
func TestMakeMigrationsGeneratedFileOmitsRedundantColumnType(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text", Unique: true},
		}},
	}}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d migration files, want 1", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}

	if strings.Contains(string(content), "[]migration.Column{migration.Column{") {
		t.Fatalf("generated migration file repeats the redundant migration.Column type inside an already-typed []migration.Column slice:\n%s", content)
	}
	if !strings.Contains(string(content), `[]migration.Column{{Name: "id"`) {
		t.Fatalf("generated migration file does not use the type-elided {Name: ...} form inside []migration.Column:\n%s", content)
	}

	aggregate, err := os.ReadFile(filepath.Join(dir, "migrations", "migrations.go"))
	if err != nil {
		t.Fatalf("read migrations.go: %v", err)
	}
	if strings.Contains(string(aggregate), "[]migration.Column{migration.Column{") {
		t.Fatalf("migrations.go repeats the redundant migration.Column type inside an already-typed []migration.Column slice:\n%s", aggregate)
	}
}

// TestMakeMigrationsAddColumnKeepsColumnType confirms the fix for
// TestMakeMigrationsGeneratedFileOmitsRedundantColumnType didn't overcorrect:
// AddColumn.Column is a single struct-typed field, not a slice element, so
// its "migration.Column" type is not redundant and must stay.
func TestMakeMigrationsAddColumnKeepsColumnType(t *testing.T) {
	dir := t.TempDir()
	first := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
	}}
	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, first); code != 0 {
		t.Fatalf("first makemigrations: exit code = %d, stderr: %s", code, stderr.String())
	}

	second := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text"},
		}},
	}}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, second); code != 0 {
		t.Fatalf("second makemigrations: exit code = %d, stderr: %s", code, stderr.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "0002_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d second migration files, want 1", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if !strings.Contains(string(content), "Column: migration.Column{Name:") {
		t.Fatalf("AddColumn's Column field must keep its migration.Column type (it is not a slice element):\n%s", content)
	}
}

// TestMakeMigrationsPreservesColumnDefaultAcrossAggregateRegeneration confirms
// a Column's Default (hand-authored on a specific migration, e.g. to backfill
// existing rows — there is no struct-tag-driven way to produce one) survives
// regenerateMigrationsAggregate's rewrite of migrations.go, which decodes
// every file's JSON metadata comment and re-emits the Go literal from that
// decoded value. If writeColumnFields ever stopped emitting Default, the
// visible (and, since it's real Go source, executed) aggregate literal would
// silently drop the default on the next unrelated makemigrations run.
func TestMakeMigrationsPreservesColumnDefaultAcrossAggregateRegeneration(t *testing.T) {
	dir := t.TempDir()
	first := dumpModelsRunner{models: []migration.Model{
		{App: "admin", Name: "admin_user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "is_staff", Type: "boolean", Default: "TRUE"},
		}},
	}}
	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, first); code != 0 {
		t.Fatalf("first makemigrations: exit code = %d, stderr: %s", code, stderr.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "0001_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d migration files, want 1", len(files))
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if !strings.Contains(string(content), `Default: "TRUE"`) {
		t.Fatalf("generated migration file does not include Default in its Go literal:\n%s", content)
	}
	if !strings.Contains(string(content), `"Default":"TRUE"`) {
		t.Fatalf("generated migration file's JSON metadata comment does not include Default:\n%s", content)
	}

	// A second, unrelated makemigrations run forces regenerateMigrationsAggregate
	// to rewrite migrations.go from scratch, decoding every existing file.
	second := dumpModelsRunner{models: []migration.Model{
		{App: "admin", Name: "admin_user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "is_staff", Type: "boolean", Default: "TRUE"},
		}},
		{App: "posts", Name: "post", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
	}}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, second); code != 0 {
		t.Fatalf("second makemigrations: exit code = %d, stderr: %s", code, stderr.String())
	}

	aggregate, err := os.ReadFile(filepath.Join(dir, "migrations", "migrations.go"))
	if err != nil {
		t.Fatalf("read migrations.go: %v", err)
	}
	if !strings.Contains(string(aggregate), `Default: "TRUE"`) {
		t.Fatalf("regenerated migrations.go dropped the Default field from an earlier AddColumn migration:\n%s", aggregate)
	}
}

func TestMakeMigrationsSecondRunOnlyAddsIncrementalChanges(t *testing.T) {
	dir := t.TempDir()

	firstRunner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
	}}
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, firstRunner); code != 0 {
		t.Fatalf("first run exit code = %d, want 0", code)
	}

	secondRunner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text"},
		}},
	}}
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, secondRunner); code != 0 {
		t.Fatalf("second run exit code = %d, want 0", code)
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d migration files, want 2", len(files))
	}

	secondFiles, err := filepath.Glob(filepath.Join(dir, "migrations", "0002_auto_*.go"))
	if err != nil {
		t.Fatalf("glob second migration: %v", err)
	}
	if len(secondFiles) != 1 {
		t.Fatalf("got %d second migration files, want 1", len(secondFiles))
	}
	secondContent, err := os.ReadFile(secondFiles[0])
	if err != nil {
		t.Fatalf("read second migration file: %v", err)
	}
	if !strings.Contains(string(secondContent), "AddColumn") {
		t.Fatalf("second migration file does not contain AddColumn step:\n%s", secondContent)
	}

	aggregate, err := os.ReadFile(filepath.Join(dir, "migrations", "migrations.go"))
	if err != nil {
		t.Fatalf("read migrations.go: %v", err)
	}
	if !strings.Contains(string(aggregate), "CreateTable") || !strings.Contains(string(aggregate), "AddColumn") {
		t.Fatalf("migrations.go does not aggregate both migrations:\n%s", aggregate)
	}
}

func TestMakeMigrationsMultiAppRunWritesOneFilePerApp(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
		{App: "posts", Name: "post", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
	}}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d migration files, want 2 (one per app)", len(files))
	}

	names := map[string]bool{}
	for _, f := range files {
		names[filepath.Base(f)] = true
	}
	for name := range names {
		if !regexp.MustCompile(`^000[12]_auto_\d{14}\.go$`).MatchString(name) {
			t.Fatalf("filename = %q, want sequential auto migration with UTC timestamp", name)
		}
	}

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Count(string(content), "App: \"users\"") == 1 && strings.Count(string(content), "App: \"posts\"") == 1 {
			t.Fatalf("file %s contains both apps' migrations, want exactly one app per file:\n%s", f, content)
		}
	}
}

func TestAutoMigrationTargetUsesUTCTimestampAndRetriesCollisions(t *testing.T) {
	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	now := time.Date(2026, time.September, 13, 12, 48, 12, 0, time.FixedZone("local", -6*60*60))
	firstName := "0001_auto_20260913184812"
	if err := os.WriteFile(filepath.Join(migrationsDir, firstName+".go"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write collision: %v", err)
	}

	name, filename, err := autoMigrationTarget(dir, 1, now)
	if err != nil {
		t.Fatalf("autoMigrationTarget: %v", err)
	}
	if name != "0001_auto_20260913184813" {
		t.Fatalf("name = %q, want UTC timestamp advanced by one second", name)
	}
	if filepath.Base(filename) != name+".go" {
		t.Fatalf("filename = %q, want stem matching migration name %q", filename, name)
	}
}

func TestAutoMigrationTargetFailsAfterFiveCollisions(t *testing.T) {
	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	now := time.Date(2026, time.September, 13, 12, 48, 12, 0, time.UTC)
	for attempt := 0; attempt < 5; attempt++ {
		name := "0001_auto_" + now.Add(time.Duration(attempt)*time.Second).Format("20060102150405") + ".go"
		if err := os.WriteFile(filepath.Join(migrationsDir, name), []byte("existing"), 0o644); err != nil {
			t.Fatalf("write collision %d: %v", attempt, err)
		}
	}

	_, _, err := autoMigrationTarget(dir, 1, now)
	if err == nil || !strings.Contains(err.Error(), "all 5 timestamp candidates already exist") {
		t.Fatalf("error = %v, want exhausted collision error", err)
	}
}

func TestSanitizeMigrationName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercase", input: "ADD_INDEX", want: "add_index"},
		{name: "spaces", input: "add author index", want: "add_author_index"},
		{name: "hyphens", input: "add-author-index", want: "add_author_index"},
		{name: "trim and collapse underscores", input: "__add___author__", want: "add_author"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeMigrationName(test.input)
			if err != nil {
				t.Fatalf("sanitizeMigrationName(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("sanitizeMigrationName(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestSanitizeMigrationNameRejectsInvalidAndEmptyNames(t *testing.T) {
	for _, input := range []string{"add@author", "", "___", " -- "} {
		t.Run(input, func(t *testing.T) {
			_, err := sanitizeMigrationName(input)
			if err == nil || !strings.Contains(err.Error(), input) {
				t.Fatalf("sanitizeMigrationName(%q) error = %v, want error naming input", input, err)
			}
		})
	}
}

func TestMakeMigrationsExplicitNameMatchesFilenameAndMigrationName(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{{
		App: "users", Name: "user", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}},
	}}}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations", "--name", " Add-Author___Indexes "}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	filename := filepath.Join(dir, "migrations", "0001_add_author_indexes.go")
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read explicit migration: %v", err)
	}
	if !strings.Contains(string(content), `Name: "0001_add_author_indexes"`) {
		t.Fatalf("migration name does not match filename:\n%s", content)
	}
}

func TestMakeMigrationsExplicitEmptyNameFails(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{{App: "users", Name: "user"}}}
	var stderr strings.Builder

	code := Run(context.Background(), []string{"makemigrations", "--name", ""}, dir, io.Discard, &stderr, runner)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "empty after sanitization") {
		t.Fatalf("stderr = %q, want empty-name error", stderr.String())
	}
}

func TestMakeMigrationsExplicitNameAppliesToEveryApp(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: []migration.Model{
		{App: "authors", Name: "author", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		{App: "posts", Name: "post", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
	}}

	var stderr strings.Builder
	if code := Run(context.Background(), []string{"makemigrations", "--name", "initial schema"}, dir, io.Discard, &stderr, runner); code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	for _, filename := range []string{"0001_initial_schema.go", "0002_initial_schema.go"} {
		if _, err := os.Stat(filepath.Join(dir, "migrations", filename)); err != nil {
			t.Fatalf("expected %s: %v", filename, err)
		}
	}

	migrations, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) != 2 || migrations[0].App == migrations[1].App || migrations[0].Name == migrations[1].Name {
		t.Fatalf("migrations = %#v, want two app-distinct migrations with sequential names", migrations)
	}
}

func TestExplicitMigrationTargetRejectsCollision(t *testing.T) {
	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	filename := filepath.Join(migrationsDir, "0002_add_index.go")
	if err := os.WriteFile(filename, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write collision: %v", err)
	}

	_, _, err := explicitMigrationTarget(dir, 2, "add_index")
	if err == nil || !strings.Contains(err.Error(), "choose a different --name") {
		t.Fatalf("error = %v, want actionable explicit-name collision error", err)
	}
}

func TestWriteMigrationFileRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "0001_named.go")
	if err := os.WriteFile(filename, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := writeMigrationFile(filename, "M0001Named", []migration.Migration{{App: "users", Name: "0001_named"}})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v, want no-overwrite error", err)
	}
	content, readErr := os.ReadFile(filename)
	if readErr != nil {
		t.Fatalf("read seeded file: %v", readErr)
	}
	if string(content) != "keep me" {
		t.Fatalf("existing file was changed: %q", content)
	}
}

func TestMakeMigrationsGeneratedMigrationsPackageCompilesAcrossMultipleFiles(t *testing.T) {
	dir := t.TempDir()

	firstRunner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
	}}
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, firstRunner); code != 0 {
		t.Fatalf("first run exit code = %d, want 0", code)
	}

	secondRunner := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text"},
		}},
	}}
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, secondRunner); code != 0 {
		t.Fatalf("second run exit code = %d, want 0", code)
	}

	// This is a regression test: every generated migration file used to
	// declare "var Migrations", which only ever compiled for exactly one
	// file. A real `go build` of the generated package is the only way to
	// catch that class of bug — decoding the JSON metadata comment (as
	// loadMigrations does) never exercises the Go source's own validity.
	migrationsDir := filepath.Join(dir, "migrations")
	// The generated package imports "github.com/angvp/tango/migration"; give
	// the throwaway module a path to the real module so it resolves locally.
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	goModContent := "module migrationstest\n\ngo 1.21\n\nrequire github.com/angvp/tango v0.0.0\n\nreplace github.com/angvp/tango => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(migrationsDir, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = migrationsDir
	var tidyOut strings.Builder
	tidy.Stdout = &tidyOut
	tidy.Stderr = &tidyOut
	if err := tidy.Run(); err != nil {
		t.Fatalf("go mod tidy for generated migrations package failed: %v\n%s", err, tidyOut.String())
	}

	build := exec.Command("go", "build", "./...")
	build.Dir = migrationsDir
	var buildOut strings.Builder
	build.Stdout = &buildOut
	build.Stderr = &buildOut
	if err := build.Run(); err != nil {
		t.Fatalf("go build of generated migrations package failed: %v\n%s", err, buildOut.String())
	}
}

func TestMakeMigrationsNoChangesWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: nil}

	var stdout strings.Builder
	code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, io.Discard, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "no changes detected") {
		t.Fatalf("stdout = %q, want it to report no changes", stdout.String())
	}

	files, _ := filepath.Glob(filepath.Join(dir, "migrations", "*.go"))
	if len(files) != 0 {
		t.Fatalf("got %d migration files, want 0", len(files))
	}
}

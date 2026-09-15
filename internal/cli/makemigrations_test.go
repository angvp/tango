package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

// TestMakeMigrationsRunFailures table-drives makemigrations's Run-level
// failure paths: argument validation, dump-models/runner failures, and
// pre-existing migration files that are corrupted or otherwise invalid.
func TestMakeMigrationsRunFailures(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		skip       func(t *testing.T) bool
		setup      func(t *testing.T, dir string)
		runner     Runner
		wantCode   int
		wantStderr string
		postCheck  func(t *testing.T, dir string)
	}{
		{
			name:     "rejects unknown flag",
			args:     []string{"makemigrations", "--bogus"},
			wantCode: 2,
		},
		{
			name:       "rejects unexpected positional arguments",
			args:       []string{"makemigrations", "extra"},
			wantCode:   2,
			wantStderr: "unexpected arguments",
		},
		{
			name:       "reports dump-models runner failure",
			args:       []string{"makemigrations"},
			runner:     erroringRunner{err: errors.New("go run failed")},
			wantCode:   1,
			wantStderr: "go run failed",
		},
		{
			name:       "reports dump-models invalid JSON",
			args:       []string{"makemigrations"},
			runner:     garbageStdoutRunner{},
			wantCode:   1,
			wantStderr: "decode model manifest",
		},
		{
			// A hand-edited or corrupted migration file (an unrecognized
			// step "kind" in its JSON metadata comment) must surface as a
			// clear makemigrations error rather than panicking or being
			// silently ignored, and regenerateMigrationsAggregate must
			// refuse the same file too.
			name: "reports corrupted existing migration file",
			args: []string{"makemigrations"},
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				corrupted := migrationJSONPrefix + `[{"app":"users","name":"0001_auto","up":[{"kind":"Bogus"}],"down":[],"reversible":true}]` + "\n"
				if err := os.WriteFile(filepath.Join(migrationsDir, "0001_auto.go"), []byte(corrupted), 0o644); err != nil {
					t.Fatalf("write corrupted migration: %v", err)
				}
			},
			wantCode:   1,
			wantStderr: "unknown migration step kind",
			postCheck: func(t *testing.T, dir string) {
				if err := regenerateMigrationsAggregate(dir); err == nil || !strings.Contains(err.Error(), "unknown migration step kind") {
					t.Fatalf("regenerateMigrationsAggregate error = %v, want unknown-step-kind error", err)
				}
			},
		},
		{
			// Covers the case where an existing migration file's JSON
			// metadata comment decodes fine but describes an impossible
			// sequence of steps (here, a DropTable with no prior
			// CreateTable) — a real hazard for a hand-edited or
			// externally-merged migration file, distinct from the
			// decode-level corruption above.
			name: "reports replay failure on semantically invalid existing migration",
			args: []string{"makemigrations"},
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				invalid := migrationJSONPrefix + `[{"app":"users","name":"0001_auto","up":[{"kind":"DropTable","table":"user"}],"down":[],"reversible":true}]` + "\n"
				if err := os.WriteFile(filepath.Join(migrationsDir, "0001_auto.go"), []byte(invalid), 0o644); err != nil {
					t.Fatalf("write invalid migration: %v", err)
				}
			},
			wantCode:   1,
			wantStderr: "does not exist",
		},
		{
			name: "reports next-migration-sequence failure when project dir is read-only",
			args: []string{"makemigrations"},
			skip: func(t *testing.T) bool { return os.Geteuid() == 0 },
			setup: func(t *testing.T, dir string) {
				if err := os.Chmod(dir, 0o555); err != nil {
					t.Fatalf("chmod project dir read-only: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			},
			runner: dumpModelsRunner{models: []migration.Model{
				{App: "users", Name: "user", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
			}},
			wantCode: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil && tt.skip(t) {
				t.Skip("root ignores directory permission bits")
			}
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			runner := tt.runner
			if runner == nil {
				runner = dumpModelsRunner{}
			}

			var stderr strings.Builder
			code := Run(context.Background(), tt.args, dir, io.Discard, &stderr, runner)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d, stderr: %s", code, tt.wantCode, stderr.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, dir)
			}
		})
	}
}

type erroringRunner struct{ err error }

func (r erroringRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	return r.err
}

type garbageStdoutRunner struct{}

func (garbageStdoutRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	_, err := stdout.Write([]byte("not json"))
	return err
}

// TestMigrationHelpersPropagateFilesystemErrors table-drives loadMigrations,
// nextMigrationSequence, autoMigrationTarget, explicitMigrationTarget, and
// writeMigrationFile all correctly surfacing an error (and, where a decoy
// error message exists, the RIGHT error rather than a look-alike) when the
// filesystem gets in the way: malformed glob patterns, unreadable files,
// permission-denied stats, corrupted metadata, and blocked paths.
func TestMigrationHelpersPropagateFilesystemErrors(t *testing.T) {
	tests := []struct {
		name               string
		skip               func(t *testing.T) bool
		setup              func(t *testing.T, dir string)
		call               func(t *testing.T, dir string) error
		wantErrContains    string
		wantErrNotContains string
	}{
		{
			name: "loadMigrations fails on malformed glob pattern",
			skip: func(t *testing.T) bool { return runtime.GOOS == "windows" },
			call: func(t *testing.T, dir string) error {
				_, err := loadMigrations(filepath.Join(dir, "weird[project"))
				return err
			},
		},
		{
			name: "loadMigrations fails when migration file is unreadable",
			skip: func(t *testing.T) bool { return os.Geteuid() == 0 },
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				filename := filepath.Join(migrationsDir, "0001_auto.go")
				if err := os.WriteFile(filename, []byte("package migrations\n"), 0o644); err != nil {
					t.Fatalf("write migration file: %v", err)
				}
				if err := os.Chmod(filename, 0o000); err != nil {
					t.Fatalf("chmod migration file: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(filename, 0o644) })
			},
			call: func(t *testing.T, dir string) error {
				_, err := loadMigrations(dir)
				return err
			},
		},
		{
			name: "loadMigrations rejects corrupted metadata comment",
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				corrupted := migrationJSONPrefix + `not valid json` + "\n"
				if err := os.WriteFile(filepath.Join(migrationsDir, "0001_auto.go"), []byte(corrupted), 0o644); err != nil {
					t.Fatalf("write corrupted migration: %v", err)
				}
			},
			call: func(t *testing.T, dir string) error {
				_, err := loadMigrations(dir)
				return err
			},
		},
		{
			// The down-steps counterpart to the "corrupted existing
			// migration file" Run-level case above (which only corrupts
			// "up"): decodeMigrations decodes Up and Down separately, so
			// each has its own error-propagation branch to exercise.
			name: "loadMigrations rejects unknown step kind in down steps",
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				corrupted := migrationJSONPrefix + `[{"app":"users","name":"0001_auto","up":[],"down":[{"kind":"Bogus"}],"reversible":true}]` + "\n"
				if err := os.WriteFile(filepath.Join(migrationsDir, "0001_auto.go"), []byte(corrupted), 0o644); err != nil {
					t.Fatalf("write corrupted migration: %v", err)
				}
			},
			call: func(t *testing.T, dir string) error {
				_, err := loadMigrations(dir)
				return err
			},
			wantErrContains: "unknown migration step kind",
		},
		{
			name: "nextMigrationSequence fails when migrations path is blocked by a file",
			setup: func(t *testing.T, dir string) {
				// Pre-create "migrations" as a plain file so MkdirAll cannot
				// create the migrations directory over it.
				if err := os.WriteFile(filepath.Join(dir, "migrations"), []byte("not a directory"), 0o644); err != nil {
					t.Fatalf("seed blocking file: %v", err)
				}
			},
			call: func(t *testing.T, dir string) error {
				_, err := nextMigrationSequence(dir)
				return err
			},
		},
		{
			name: "nextMigrationSequence fails on malformed glob pattern",
			skip: func(t *testing.T) bool { return runtime.GOOS == "windows" },
			call: func(t *testing.T, dir string) error {
				_, err := nextMigrationSequence(filepath.Join(dir, "weird[project"))
				return err
			},
		},
		{
			name: "autoMigrationTarget fails on permission-denied stat",
			skip: func(t *testing.T) bool { return os.Geteuid() == 0 },
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				if err := os.Chmod(migrationsDir, 0o000); err != nil {
					t.Fatalf("chmod migrations: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(migrationsDir, 0o755) })
			},
			call: func(t *testing.T, dir string) error {
				_, _, err := autoMigrationTarget(dir, 1, time.Now().UTC())
				return err
			},
			wantErrNotContains: "all 5 timestamp candidates",
		},
		{
			name: "explicitMigrationTarget fails on permission-denied stat",
			skip: func(t *testing.T) bool { return os.Geteuid() == 0 },
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "migrations")
				if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
					t.Fatalf("mkdir migrations: %v", err)
				}
				if err := os.Chmod(migrationsDir, 0o000); err != nil {
					t.Fatalf("chmod migrations: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(migrationsDir, 0o755) })
			},
			call: func(t *testing.T, dir string) error {
				_, _, err := explicitMigrationTarget(dir, 1, "add_index")
				return err
			},
			wantErrNotContains: "already exists",
		},
		{
			name: "writeMigrationFile fails when parent directory is missing",
			call: func(t *testing.T, dir string) error {
				filename := filepath.Join(dir, "does-not-exist", "0001_named.go")
				return writeMigrationFile(filename, "M0001Named", []migration.Migration{{App: "users", Name: "0001_named"}})
			},
			wantErrNotContains: "already exists",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil && tt.skip(t) {
				t.Skip("root ignores directory/file permission bits")
			}
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}

			err := tt.call(t, dir)
			if err == nil {
				t.Fatal("error = nil, want non-nil")
			}
			if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErrContains)
			}
			if tt.wantErrNotContains != "" && strings.Contains(err.Error(), tt.wantErrNotContains) {
				t.Fatalf("error = %v, want it NOT to contain %q", err, tt.wantErrNotContains)
			}
		})
	}
}

// TestMakeMigrationsGeneratesUniqueAndIndexStepsAcrossRuns exercises the
// AlterColumnUnique/CreateIndex/DropIndex Go-literal and JSON encode/decode
// paths, which a single create-table migration never touches.
func TestMakeMigrationsGeneratesUniqueAndIndexStepsAcrossRuns(t *testing.T) {
	dir := t.TempDir()

	first := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text"},
		}},
	}}
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, first); code != 0 {
		t.Fatalf("first run exit code = %d, want 0", code)
	}

	// Second run: make email unique and indexed.
	second := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text", Unique: true, Indexed: true},
		}},
	}}
	var stderr strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, &stderr, second); code != 0 {
		t.Fatalf("second run exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	secondFile := mustReadSingleGlob(t, filepath.Join(dir, "migrations", "0002_*.go"))
	if !strings.Contains(secondFile, "migration.AlterColumnUnique{Table: \"user\", Column: \"email\", Unique: true}") {
		t.Fatalf("second migration missing AlterColumnUnique literal:\n%s", secondFile)
	}
	if !strings.Contains(secondFile, "migration.CreateIndex{Table: \"user\", Column: \"email\"}") {
		t.Fatalf("second migration missing CreateIndex literal:\n%s", secondFile)
	}

	// Third run: drop the index again (unique stays), to hit DropIndex.
	third := dumpModelsRunner{models: []migration.Model{
		{App: "users", Name: "user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text", Unique: true, Indexed: false},
		}},
	}}
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, &stderr, third); code != 0 {
		t.Fatalf("third run exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	thirdFile := mustReadSingleGlob(t, filepath.Join(dir, "migrations", "0003_*.go"))
	if !strings.Contains(thirdFile, "migration.DropIndex{Table: \"user\", Column: \"email\"}") {
		t.Fatalf("third migration missing DropIndex literal:\n%s", thirdFile)
	}

	// The round trip through loadMigrations (used by both the next
	// makemigrations run and regenerateMigrationsAggregate) must also decode
	// these step kinds back correctly.
	migrations, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) != 3 {
		t.Fatalf("loaded %d migrations, want 3", len(migrations))
	}
}

func mustReadSingleGlob(t *testing.T, pattern string) string {
	t.Helper()
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(files) != 1 {
		t.Fatalf("glob %s matched %d files, want 1", pattern, len(files))
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read %s: %v", files[0], err)
	}
	return string(content)
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

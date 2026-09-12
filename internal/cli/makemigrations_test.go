package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	if filepath.Base(files[0]) != "0001_auto.go" {
		t.Fatalf("filename = %q, want %q", filepath.Base(files[0]), "0001_auto.go")
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

	secondContent, err := os.ReadFile(filepath.Join(dir, "migrations", "0002_auto.go"))
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
	if !names["0001_auto.go"] || !names["0002_auto.go"] {
		t.Fatalf("filenames = %v, want 0001_auto.go and 0002_auto.go", names)
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

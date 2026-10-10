package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

func headlineModel(columns ...migration.Column) dumpModelsRunner {
	id := migration.Column{Name: "id", Type: "integer", PrimaryKey: true}
	return dumpModelsRunner{models: []migration.Model{
		{App: "news", Name: "headline", Struct: "Headline", Columns: append([]migration.Column{id}, columns...)},
	}}
}

func readAll(t *testing.T, pattern string) string {
	t.Helper()
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		t.Fatalf("glob %s: %v (%d files)", pattern, err, len(files))
	}
	var all strings.Builder
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(content)
	}
	return all.String()
}

func TestMakeMigrationsWritesTheLengthOfABoundedColumnAndIsQuietAfterwards(t *testing.T) {
	dir := t.TempDir()
	title := migration.Column{Name: "title", Type: "varchar", Length: 200}
	body := migration.Column{Name: "body", Type: "text"}
	run := func(runner dumpModelsRunner) (int, string) {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner)
		return code, stdout.String() + stderr.String()
	}

	if code, out := run(headlineModel(title, body)); code != 0 {
		t.Fatalf("first run: %d\n%s", code, out)
	}
	first := readAll(t, filepath.Join(dir, "migrations", "0001_*.go"))
	if !strings.Contains(first, `Name: "title", Type: "varchar", Length: 200`) {
		t.Fatalf("CreateTable literal lacks the length:\n%s", first)
	}
	if strings.Contains(first, `Name: "body", Type: "text", Length`) || strings.Contains(first, `"Length":0`) {
		t.Fatalf("an unbounded column must not mention a length:\n%s", first)
	}
	aggregate := readAll(t, filepath.Join(dir, "migrations", "migrations.go"))
	if !strings.Contains(aggregate, `Type: "varchar", Length: 200`) {
		t.Fatalf("aggregate dropped the length:\n%s", aggregate)
	}

	code, out := run(headlineModel(title, body))
	if code != 0 || !strings.Contains(out, "no changes detected") {
		t.Fatalf("second run = %d %q, want no changes detected", code, out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if len(files) != 1 {
		t.Fatalf("got %d migration files, want 1", len(files))
	}

	slug := migration.Column{Name: "slug", Type: "varchar", Length: 5}
	if code, out := run(headlineModel(title, body, slug)); code != 0 {
		t.Fatalf("third run: %d\n%s", code, out)
	}
	if second := readAll(t, filepath.Join(dir, "migrations", "0002_*.go")); !strings.Contains(second, `Name: "slug", Type: "varchar", Length: 5`) {
		t.Fatalf("AddColumn literal lacks the length:\n%s", second)
	}

	state, err := loadMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := migration.Replay(state)
	if err != nil {
		t.Fatal(err)
	}
	var lengths []int
	for _, column := range replayed.Tables["headline"].Columns {
		lengths = append(lengths, column.Length)
	}
	if want := []int{0, 200, 0, 5}; !reflect.DeepEqual(lengths, want) {
		t.Fatalf("replayed lengths = %v (tables %v), want %v", lengths, replayed.Tables, want)
	}
}

func TestAlterColumnTypeLengthsSurviveTheHeaderAndTheLiteral(t *testing.T) {
	step := migration.AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 100, To: "text"}
	encoded, err := encodeSteps([]migration.Step{step})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeSteps(encoded)
	if err != nil || decoded[0] != step {
		t.Fatalf("decoded = %#v, %v; want %#v", decoded, err, step)
	}
	var literal strings.Builder
	writeStepLiteral(&literal, step)
	if want := `migration.AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 100, To: "text"},`; !strings.Contains(literal.String(), want) {
		t.Fatalf("literal = %s, want %s", literal.String(), want)
	}
	var plain strings.Builder
	writeStepLiteral(&plain, migration.AlterColumnType{Table: "t", Column: "c", From: "integer", To: "real"})
	if strings.Contains(plain.String(), "Length") {
		t.Fatalf("a step without lengths must not mention them: %s", plain.String())
	}
}

func TestMakeMigrationsWritesAWideningAndIsQuietAfterwards(t *testing.T) {
	dir := t.TempDir()
	run := func(column migration.Column) string {
		var stdout, stderr strings.Builder
		if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, headlineModel(column)); code != 0 {
			t.Fatalf("makemigrations: %d\n%s%s", code, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	run(migration.Column{Name: "title", Type: "varchar", Length: 100})
	run(migration.Column{Name: "title", Type: "varchar", Length: 200})
	run(migration.Column{Name: "title", Type: "text"})

	second := readAll(t, filepath.Join(dir, "migrations", "0002_*.go"))
	if want := `migration.AlterColumnType{Table: "headline", Column: "title", From: "varchar", FromLength: 100, To: "varchar", ToLength: 200}`; !strings.Contains(second, want) || strings.Contains(second, "Reversible: true") {
		t.Fatalf("0002 lacks %s or is reversible:\n%s", want, second)
	}
	third := readAll(t, filepath.Join(dir, "migrations", "0003_*.go"))
	if want := `From: "varchar", FromLength: 200, To: "text"`; !strings.Contains(third, want) {
		t.Fatalf("0003 lacks %s:\n%s", want, third)
	}
	if out := run(migration.Column{Name: "title", Type: "text"}); !strings.Contains(out, "no changes detected") {
		t.Fatalf("fourth run = %q, want no changes detected", out)
	}
}

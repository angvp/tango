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

func commentModel(indexes ...migration.CompositeIndex) []migration.Model {
	return []migration.Model{{App: "board", Name: "comment", Struct: "Comment", Columns: []migration.Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "post_id", Type: "integer"},
		{Name: "author", Type: "text"},
		{Name: "created_at", Type: "timestamp"},
	}, Fields: map[string]string{"post_id": "PostID", "created_at": "CreatedAt"}, Indexes: indexes}}
}

func runMakemigrations(t *testing.T, dir string, models []migration.Model) string {
	t.Helper()
	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, dumpModelsRunner{models: models}); code != 0 {
		t.Fatalf("makemigrations exit code = %d, stderr: %s", code, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func lastMigrationFile(t *testing.T, dir string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "migrations", "[0-9][0-9][0-9][0-9]_*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migration files in %s (%v)", dir, err)
	}
	content, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestMakemigrationsWritesCompositeIndexSteps(t *testing.T) {
	dir := t.TempDir()
	models := commentModel(
		migration.CompositeIndex{Columns: []string{"post_id", "created_at"}},
		migration.CompositeIndex{Columns: []string{"post_id", "author"}, Unique: true, Name: "uniq_comment_once"},
	)
	runMakemigrations(t, dir, models)

	content := lastMigrationFile(t, dir)
	for _, want := range []string{
		`migration.CreateCompositeIndex{Table: "comment", Columns: []string{"post_id", "created_at"}},`,
		`migration.CreateCompositeIndex{Table: "comment", Columns: []string{"post_id", "author"}, Unique: true, Name: "uniq_comment_once"},`,
		`"kind":"CreateCompositeIndex"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated file lacks %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "CompositeIndexV2") {
		t.Fatalf("a composite step has a kind of its own, never a V2 suffix:\n%s", content)
	}

	decoded, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("read the header back: %v", err)
	}
	wantUp := []migration.Step{
		migration.CreateCompositeIndex{Table: "comment", Columns: []string{"post_id", "created_at"}},
		migration.CreateCompositeIndex{Table: "comment", Columns: []string{"post_id", "author"}, Unique: true, Name: "uniq_comment_once"},
	}
	if got := decoded[0].Up[1:]; !reflect.DeepEqual(got, wantUp) {
		t.Fatalf("header decodes to %#v, want %#v", got, wantUp)
	}
	if got := runMakemigrations(t, dir, models); got != "no changes detected" {
		t.Fatalf("second run = %q, want no changes detected", got)
	}
}

func TestMakemigrationsDropsARemovedCompositeIndexAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	both := commentModel(
		migration.CompositeIndex{Columns: []string{"post_id", "created_at"}},
		migration.CompositeIndex{Columns: []string{"post_id", "author"}, Unique: true},
	)
	runMakemigrations(t, dir, both)
	reordered := commentModel(both[0].Indexes[1], both[0].Indexes[0])
	if got := runMakemigrations(t, dir, reordered); got != "no changes detected" {
		t.Fatalf("reordering the declarations = %q, want no changes detected", got)
	}
	runMakemigrations(t, dir, commentModel(both[0].Indexes[0]))
	content := lastMigrationFile(t, dir)
	for _, want := range []string{
		`migration.DropCompositeIndex{Table: "comment", Columns: []string{"post_id", "author"}, Unique: true},`,
		`migration.CreateCompositeIndex{Table: "comment", Columns: []string{"post_id", "author"}, Unique: true},`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated file lacks %q:\n%s", want, content)
		}
	}
	decoded, err := loadMigrations(dir)
	if err != nil || len(decoded) != 2 || !decoded[1].Reversible {
		t.Fatalf("loadMigrations = %v, %v; want two migrations, the second reversible", decoded, err)
	}
}

func TestMakemigrationsRenamesADerivedCompositeIndexAfterAColumnRename(t *testing.T) {
	dir := t.TempDir()
	runMakemigrations(t, dir, commentModel(migration.CompositeIndex{Columns: []string{"post_id", "created_at"}}))
	renamed := commentModel(migration.CompositeIndex{Columns: []string{"thread_id", "created_at"}})
	renamed[0].Columns[1].Name = "thread_id"
	renamed[0].Fields = map[string]string{"thread_id": "ThreadID", "created_at": "CreatedAt"}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"makemigrations", "--rename", "board.comment.post_id=thread_id"}, dir, &stdout, &stderr, dumpModelsRunner{models: renamed})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	content := lastMigrationFile(t, dir)
	rename := strings.Index(content, `migration.RenameColumn{Table: "comment", From: "post_id", To: "thread_id"}`)
	index := strings.Index(content, `migration.RenameCompositeIndex{Table: "comment", Columns: []string{"thread_id", "created_at"}, From: "idx_comment_post_id_created_at", To: "idx_comment_thread_id_created_at"}`)
	if rename == -1 || index == -1 || index < rename {
		t.Fatalf("want the index rename after the column rename in:\n%s", content)
	}
}

// TestCompositeStepKindsAreNewKindsAnOlderReaderRefusesByName pins the header
// vocabulary: the three composite steps are kinds of their own, never a V2
// suffix on an older kind, so a reader that predates them fails with
// "unknown migration step kind" instead of reading half a step.
func TestCompositeStepKindsAreNewKindsAnOlderReaderRefusesByName(t *testing.T) {
	for _, step := range []migration.Step{
		migration.CreateCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		migration.DropCompositeIndex{Table: "t", Columns: []string{"a", "b"}},
		migration.RenameCompositeIndex{Table: "t", Columns: []string{"a", "b"}, From: "x", To: "y"},
	} {
		encoded, err := encodeSteps([]migration.Step{step})
		if err != nil {
			t.Fatal(err)
		}
		kind := encoded[0].Kind
		if !isNewStepKind(kind) || strings.HasSuffix(kind, v2Suffix) {
			t.Fatalf("kind %q must be a new kind without a %s suffix", kind, v2Suffix)
		}
	}
	if _, err := decodeStep(encodedStep{Kind: "CreateTupleIndex"}); err == nil || !strings.Contains(err.Error(), "unknown migration step kind") {
		t.Fatalf("decode of a kind no reader knows = %v, want the unknown-kind refusal", err)
	}
}

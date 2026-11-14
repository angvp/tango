package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

func exactSchemaModels() []migration.Model {
	return []migration.Model{
		{App: "blog", Name: "users", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
			{Name: "email", Type: "varchar", Length: 255, NotNull: true, Unique: true, UniqueName: "idx_users_email"},
			{Name: "language", Type: "varchar", Length: 5, DefaultKind: "string", DefaultValue: "en"},
			{Name: "slug", Type: "text", Indexed: true, IndexName: "ix_users_slug"},
			{Name: "created_at", Type: "timestamp", Naive: true, NotNull: true},
		}},
	}
}

// TestMakeMigrationsWritesExactSchemaFields checks the exact-schema fields
// reach both the Go literal and the JSON metadata, and that replaying the
// written file yields no further migration.
func TestMakeMigrationsWritesExactSchemaFields(t *testing.T) {
	dir := t.TempDir()
	runner := dumpModelsRunner{models: exactSchemaModels()}
	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner); code != 0 {
		t.Fatalf("makemigrations: exit %d, stderr: %s", code, stderr.String())
	}
	files, err := filepath.Glob(filepath.Join(dir, "migrations", "0001_*.go"))
	if err != nil || len(files) != 1 {
		t.Fatalf("glob = %v, %v", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{
		`Width: 32`, `NotNull: true`, `UniqueName: "idx_users_email"`,
		`DefaultKind: "string", DefaultValue: "en"`, `IndexName: "ix_users_slug"`, `Naive: true`,
		`"Width":32`, `"NotNull":true`, `"UniqueName":"idx_users_email"`, `"DefaultValue":"en"`, `"Naive":true`,
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("migration file lacks %s:\n%s", want, content)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, runner); code != 0 {
		t.Fatalf("second makemigrations: exit %d, stderr: %s", code, stderr.String())
	}
	again, _ := filepath.Glob(filepath.Join(dir, "migrations", "0002_*.go"))
	if len(again) != 0 {
		t.Fatalf("second run wrote %v, want no change", again)
	}
}

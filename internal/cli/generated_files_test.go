package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/internal/migrationcompat"
)

// rawDumpRunner answers -tango-dump-models with a recorded output.
type rawDumpRunner struct{ output []byte }

func (r rawDumpRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	_, err := stdout.Write(r.output)
	return err
}

// TestReleasedGeneratorHeadersReadBack enforces the header half of the
// Generated-file contract: the // tango:migration-json header of every file
// a released generator wrote decodes to exactly the steps its Go literal
// compiles to, and makemigrations, given that release's own app's
// -tango-dump-models output for the models the files were generated from,
// replays the headers to a schema with nothing left to generate.
func TestReleasedGeneratorHeadersReadBack(t *testing.T) {
	for _, gen := range migrationcompat.Generators {
		compiled := gen.Migrations
		t.Run(gen.Dir, func(t *testing.T) {
			fixture := filepath.Join("..", "migrationcompat", gen.Dir)

			decoded, err := loadMigrations(fixture)
			if err != nil {
				t.Fatalf("read headers: %v", err)
			}
			if !reflect.DeepEqual(decoded, compiled) {
				t.Fatalf("headers decode to\n%#v\nwant the compiled migrations\n%#v", decoded, compiled)
			}

			dir := t.TempDir()
			if err := os.CopyFS(dir, os.DirFS(fixture)); err != nil {
				t.Fatalf("copy fixture: %v", err)
			}
			models, err := os.ReadFile(filepath.Join(fixture, "models.json"))
			if err != nil {
				t.Fatalf("read models.json: %v", err)
			}
			var stdout, stderr strings.Builder
			if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &stderr, rawDumpRunner{output: models}); code != 0 {
				t.Fatalf("makemigrations exit code = %d\nstderr: %s", code, stderr.String())
			}
			if got := strings.TrimSpace(stdout.String()); got != "no changes detected" {
				t.Fatalf("makemigrations output = %q, want no changes detected", got)
			}
		})
	}
}

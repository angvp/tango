package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWarnOldLibrary(t *testing.T) {
	cases := []struct {
		name  string
		gomod string
		warns bool
	}{
		{"single line below", "module x\nrequire github.com/angvp/tango v0.9.0\n", true},
		{"block below", "module x\nrequire (\n\tgithub.com/other/z v1.0.0\n\tgithub.com/angvp/tango v0.9.0\n)\n", true},
		{"indirect below", "module x\nrequire (\n\tgithub.com/angvp/tango v0.9.0 // indirect\n)\n", true},
		{"pseudo below", "module x\nrequire github.com/angvp/tango v0.9.1-0.20260101000000-abcdefabcdef\n", true},
		{"exact", "module x\nrequire github.com/angvp/tango v0.10.0\n", false},
		{"above", "module x\nrequire github.com/angvp/tango v0.11.0\n", false},
		{"pseudo of exact", "module x\nrequire github.com/angvp/tango v0.10.0-0.20260101000000-abcdefabcdef\n", false},
		{"replace only", "module x\nreplace github.com/angvp/tango => ../tango\n", false},
		{"require with replace", "module x\nrequire github.com/angvp/tango v0.0.0\nreplace github.com/angvp/tango => ../tango\n", false},
		{"replace block", "module x\nreplace (\n\tgithub.com/angvp/tango v0.9.0 => ../tango\n)\n", false},
		{"no requirement", "module x\nrequire github.com/other/z v1.0.0\n", false},
		{"unparsable", "module x\nrequire github.com/angvp/tango vNEXT\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(c.gomod), 0o644); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			warnOldLibrary(dir, &stderr)
			if got := strings.Contains(stderr.String(), "warning: this app requires github.com/angvp/tango"); got != c.warns {
				t.Fatalf("warned = %v, want %v; stderr %q", got, c.warns, stderr.String())
			}
		})
	}
}

func TestWarnOldLibraryLooksInParentDirectoriesAndStaysSilentWithoutGoMod(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "go.mod"), []byte("module x\nrequire github.com/angvp/tango v0.9.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(parent, "cmd", "app")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	warnOldLibrary(child, &stderr)
	if !strings.Contains(stderr.String(), "v0.9.0") {
		t.Fatalf("stderr = %q, want a warning naming v0.9.0", stderr.String())
	}
	stderr.Reset()
	warnOldLibrary(t.TempDir(), &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want silence without a go.mod", stderr.String())
	}
}

type handshakeRunner struct {
	args   []string
	stderr string
}

func (r *handshakeRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	r.args = append([]string{name}, args...)
	_, _ = stdout.Write([]byte("[]"))
	_, _ = io.WriteString(stderr, r.stderr)
	return nil
}

// TestMakemigrationsSendsTheHandshakeToken pins the argv: a v0.9.0 app parses
// the trailing positional token and ignores it, which is why it is not a flag.
func TestMakemigrationsSendsTheHandshakeToken(t *testing.T) {
	runner := &handshakeRunner{}
	dir := t.TempDir()
	if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, io.Discard, runner); code != 0 {
		t.Fatalf("Run = %d", code)
	}
	want := []string{"go", "run", ".", "-tango-dump-models", "tango-exact-schema-v1", "tango-composite-index-v1"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("argv = %q, want %q", runner.args, want)
	}

	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dump := fs.Bool("tango-dump-models", false, "")
	if err := fs.Parse(runner.args[3:]); err != nil || !*dump {
		t.Fatalf("a v0.9.0 flag set parsing the argv: err %v, dump %v", err, *dump)
	}
}

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// documentRunner answers `go run . -tango-openapi` with a fixed document.
type documentRunner struct {
	args []string
	out  string
	err  error
}

func (r *documentRunner) Run(_ context.Context, _ string, _ string, args []string, stdout io.Writer, _ io.Writer) error {
	r.args = append([]string(nil), args...)
	if r.err != nil {
		return r.err
	}
	_, err := io.WriteString(stdout, r.out)
	return err
}

func TestOpenAPIPrintsTheAppsDocument(t *testing.T) {
	runner := &documentRunner{out: "{\"openapi\":\"3.1.0\"}\n"}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"openapi"}, t.TempDir(), &stdout, &stderr, runner); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if got := strings.Join(runner.args, " "); got != "run . -tango-openapi" {
		t.Fatalf("ran go %q", got)
	}
	if stdout.String() != runner.out {
		t.Fatalf("stdout %q, want the document", stdout.String())
	}
}

func TestOpenAPIWritesAFileAndKeepsTheOldOneWhenTheAppFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "openapi.json")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"openapi", "-o", "openapi.json"}, dir, &stdout, &stderr, &documentRunner{out: "NEW\n"}); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if got, _ := os.ReadFile(target); string(got) != "NEW\n" {
		t.Fatalf("file holds %q", got)
	}
	if !strings.Contains(stdout.String(), "wrote openapi.json") {
		t.Fatalf("stdout %q", stdout.String())
	}
	failing := &documentRunner{out: "PARTIAL", err: io.ErrUnexpectedEOF}
	if code := Run(context.Background(), []string{"openapi", "-o", "openapi.json"}, dir, io.Discard, io.Discard, failing); code == 0 {
		t.Fatal("a failed run exited 0")
	}
	if got, _ := os.ReadFile(target); string(got) != "NEW\n" {
		t.Fatalf("a failed run changed the file to %q", got)
	}
}

func TestOpenAPIRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{{"openapi", "-o"}, {"openapi", "--nope"}} {
		var stderr bytes.Buffer
		if code := Run(context.Background(), args, t.TempDir(), io.Discard, &stderr, &documentRunner{}); code != 2 || stderr.Len() == 0 {
			t.Errorf("%v: exit %d, stderr %q; want exit 2 with a message", args, code, stderr.String())
		}
	}
}

// TestOpenAPIRunsAgainstAScaffoldedProject scaffolds a project, installs the
// openapi app the way the guide says, and exports the document twice.
func TestOpenAPIRunsAgainstAScaffoldedProject(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and runs a project")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	runner := localTangoRunner{repo: repo}
	var stderr strings.Builder
	if code := Run(context.Background(), []string{"newproject", "shop"}, dir, io.Discard, &stderr, runner); code != 0 {
		t.Fatalf("newproject exit %d: %s", code, stderr.String())
	}
	project := filepath.Join(dir, "shop")

	// Without the app installed the command says what to do.
	stderr.Reset()
	if code := Run(context.Background(), []string{"openapi"}, project, io.Discard, &stderr, runner); code == 0 || !strings.Contains(stderr.String(), "openapi.New") {
		t.Fatalf("exit %d, stderr %q: want a failure naming openapi.New", code, stderr.String())
	}

	file := filepath.Join(project, "project", "project.go")
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(source), "\t\tadmin.New(store),", "\t\topenapi.New(openapi.Options{Title: \"Shop\"}),\n\t\tadmin.New(store),", 1)
	edited = strings.Replace(edited, "\"github.com/angvp/tango/admin\"", "\"github.com/angvp/tango/admin\"\n\t\"github.com/angvp/tango/openapi\"", 1)
	if edited == string(source) {
		t.Fatal("the scaffold's InstalledApps changed shape; update this test")
	}
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		stderr.Reset()
		if code := Run(context.Background(), []string{"openapi"}, project, out, &stderr, runner); code != 0 {
			t.Fatalf("openapi exit %d: %s", code, stderr.String())
		}
	}
	if first.Len() == 0 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("export is empty or differs between runs:\n%s", first.String())
	}
	if !strings.Contains(first.String(), `"title": "Shop"`) || !strings.Contains(first.String(), `"openapi": "3.1.0"`) {
		t.Fatalf("not the shop's document:\n%s", first.String())
	}
}

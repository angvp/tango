package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shellRunner records the commands `tango shell` runs and answers each with
// the error func returns for it.
type shellRunner struct {
	commands []recordedCommand
	answer   func(name string, args []string) error
}

func (r *shellRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	r.commands = append(r.commands, recordedCommand{dir: dir, name: name, args: append([]string(nil), args...)})
	if r.answer != nil {
		return r.answer(name, args)
	}
	return nil
}

func projectWithShell(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shell", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestShellWithoutAShellProgramNamesTheMissingFileAndTheGuide(t *testing.T) {
	runner := &shellRunner{}
	var stderr strings.Builder
	code := Run(context.Background(), []string{"shell"}, t.TempDir(), io.Discard, &stderr, runner)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	for _, want := range []string{"shell/main.go", "docs/guides/shell.md"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to mention %q", stderr.String(), want)
		}
	}
	if len(runner.commands) != 0 {
		t.Fatalf("ran %v, want nothing run for a project without a shell", runner.commands)
	}
}

func TestShellBuildsTheProjectShellAndRunsItWithTheArguments(t *testing.T) {
	dir := projectWithShell(t)
	runner := &shellRunner{}
	code := Run(context.Background(), []string{"shell", "--readonly", "-c", "Models()"}, dir, io.Discard, io.Discard, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands = %+v, want a build and a run", runner.commands)
	}
	build, run := runner.commands[0], runner.commands[1]
	if build.name != "go" || len(build.args) != 4 || build.args[0] != "build" || build.args[1] != "-o" || build.args[3] != "./shell" || build.dir != dir {
		t.Fatalf("build = %+v, want go build -o <binary> ./shell in the project", build)
	}
	if run.name != build.args[2] || run.dir != dir || strings.Join(run.args, " ") != "--readonly -c Models()" {
		t.Fatalf("run = %+v, want the built binary with the arguments forwarded unchanged", run)
	}
}

func TestShellPassesOnTheShellsOwnExitCode(t *testing.T) {
	for _, want := range []int{2, 130} {
		dir := projectWithShell(t)
		runner := &shellRunner{answer: func(name string, args []string) error {
			if name == "go" {
				return nil
			}
			return exitError(t, want)
		}}
		if code := Run(context.Background(), []string{"shell"}, dir, io.Discard, io.Discard, runner); code != want {
			t.Errorf("exit code = %d, want the shell's %d", code, want)
		}
	}
}

func TestShellStopsWhenTheProjectShellDoesNotBuild(t *testing.T) {
	dir := projectWithShell(t)
	runner := &shellRunner{answer: func(name string, args []string) error { return exitError(t, 1) }}
	if code := Run(context.Background(), []string{"shell"}, dir, io.Discard, io.Discard, runner); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want the shell not run after a failed build", runner.commands)
	}
}

func TestShellRemovesTheBinaryItBuilt(t *testing.T) {
	dir := projectWithShell(t)
	var built string
	runner := &shellRunner{answer: func(name string, args []string) error {
		if name == "go" {
			built = args[2]
			return os.WriteFile(built, []byte("x"), 0o755)
		}
		return nil
	}}
	if code := Run(context.Background(), []string{"shell"}, dir, io.Discard, io.Discard, runner); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if _, err := os.Stat(built); !os.IsNotExist(err) {
		t.Fatalf("the built shell %s still exists (stat err %v)", built, err)
	}
}

func TestUsageListsTheShell(t *testing.T) {
	var out strings.Builder
	Run(context.Background(), []string{"help"}, t.TempDir(), &out, io.Discard, &shellRunner{})
	if !strings.Contains(out.String(), "tango shell [-c EXPR] [--readonly]") || strings.Contains(out.String(), "Not implemented") {
		t.Fatalf("usage = %q, want the shell documented as a command", out.String())
	}
}

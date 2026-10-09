//go:build unix

package cli

import (
	"errors"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestShellOnATerminal drives the real, scaffolded shell on a pseudo-terminal:
// prompt, multi-line input, Ctrl-C, Ctrl-D, history and the exit codes. A
// Python script plays the person at the keyboard (Python ships with every
// system this runs on, and its pty module is the portable way to get a
// terminal in a test).
func TestShellOnATerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, builds and runs a project")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed; it drives the pseudo-terminal")
	}
	project, _ := migratedSQLiteProject(t, "blog")
	binary := buildShell(t, project)
	home := t.TempDir()

	driver, err := filepath.Abs(filepath.Join("testdata", "shell_pty_driver.py"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(python, "-I", driver, binary, project, home).CombinedOutput()
	if err != nil {
		t.Fatalf("terminal session failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// Ctrl-C during an evaluation ends a non-interactive run with status 130 too:
// the interpreter cannot interrupt a running evaluation.
func TestShellInterruptedDuringAnEvaluationExits130WithoutATerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, builds and runs a project")
	}
	project, _ := migratedSQLiteProject(t, "blog")
	binary := buildShell(t, project)
	cmd := exec.Command(binary, "-c", "for { }")
	cmd.Dir = project
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second) // let the loop start; the handler is installed before it
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 130 {
		t.Fatalf("shell -c after SIGINT: %v, want exit status 130", err)
	}
}

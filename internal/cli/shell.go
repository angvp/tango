package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
)

// shellGuide is where a person is sent when their project has no shell yet.
const shellGuide = "https://github.com/angvp/tango/blob/main/docs/guides/shell.md#adding-the-shell-to-an-existing-project"

// shellCommand implements `tango shell`: it builds the project's own
// shell/main.go and runs it with args, so the shell links the project's apps
// and models, which this process cannot. The `tango` binary itself never
// links the interpreter.
//
// The shell is built and run directly rather than through `go run`, which
// reports every non-zero exit as status 1 and so would lose the shell's own
// exit codes (2 for a bad command line, 130 for Ctrl-C).
func shellCommand(ctx context.Context, runner Runner, dir string, args []string, stdout io.Writer, stderr io.Writer) int {
	if _, err := os.Stat(filepath.Join(dir, "shell", "main.go")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "tango shell: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "tango shell: this project has no shell/main.go.\n"+
			"  Projects created by tango newproject have one; for an older project, add it as described in %s\n", shellGuide)
		return 1
	}

	work, err := os.MkdirTemp("", "tango-shell-")
	if err != nil {
		fmt.Fprintf(stderr, "tango shell: %v\n", err)
		return 1
	}
	defer os.RemoveAll(work)
	binary := filepath.Join(work, "shell")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if code := runGo(ctx, runner, dir, stdout, stderr, []string{"build", "-o", binary, "./shell"}); code != 0 {
		return code
	}

	// Ctrl-C belongs to the shell: it clears the prompt line, or ends a
	// running evaluation. This process must outlive it to pass on the exit code.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	done := make(chan struct{})
	defer func() { signal.Stop(interrupts); close(done) }()
	go func() {
		for {
			select {
			case <-interrupts:
			case <-done:
				return
			}
		}
	}()

	if err := runner.Run(ctx, dir, binary, args, stdout, stderr); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			if code := exitError.ExitCode(); code >= 0 {
				return code
			}
			return 1 // the shell was killed by a signal
		}
		fmt.Fprintf(stderr, "tango shell: %v\n", err)
		return 1
	}
	return 0
}

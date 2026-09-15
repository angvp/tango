package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type recordedCommand struct {
	dir  string
	name string
	args []string
}

type recordingRunner struct {
	command recordedCommand
	err     error
}

func (r *recordingRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	r.command = recordedCommand{
		dir:  dir,
		name: name,
		args: append([]string(nil), args...),
	}
	return r.err
}

func TestRunCommandWrapsGoRunDot(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"run", "--", "extra"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "--", "extra"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestCheckCommandRunsAppWithCheckFlag(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"check"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-check"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestUnknownCommandReturnsUsageError(t *testing.T) {
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"wat"}, "/app", io.Discard, &stderr, &recordingRunner{})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "wat"`) {
		t.Fatalf("stderr = %q, want unknown command message", stderr.String())
	}
}

func TestUsageDocumentsMakeMigrationsNameFlag(t *testing.T) {
	var stdout bytes.Buffer
	code := Run(context.Background(), []string{"help"}, "/app", &stdout, io.Discard, &recordingRunner{})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "tango makemigrations [--name <name>]") {
		t.Fatalf("usage = %q, want makemigrations --name documentation", stdout.String())
	}
}

func TestShellDocumentsDeferredImplementation(t *testing.T) {
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"shell"}, "/app", io.Discard, &stderr, &recordingRunner{})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Yaegi") {
		t.Fatalf("stderr = %q, want Yaegi direction", stderr.String())
	}
}

func TestMigrateCommandRunsAppWithMigrateFlag(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"migrate"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-migrate"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestMigrateDownRunsAppWithMigrateAndDownFlags(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"migrate", "down"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-migrate", "-down"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestAdminCreateRunsAppWithCreateFlag(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"admin", "create", "alice"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-tango-admin-create=alice"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestAdminResetPasswordRunsAppWithResetPasswordFlag(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"admin", "resetpassword", "alice"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-tango-admin-resetpassword=alice"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestAdminDeactivateRunsAppWithDeactivateFlag(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"admin", "deactivate", "alice"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-tango-admin-deactivate=alice"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestAdminCreateWithNoStaffAndNoSuperuserFlags(t *testing.T) {
	runner := &recordingRunner{}
	code := Run(context.Background(), []string{"admin", "create", "alice", "--no-staff", "--no-superuser"}, "/app", io.Discard, io.Discard, runner)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-tango-admin-create=alice", "-tango-admin-no-staff", "-tango-admin-no-superuser"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestAdminCreateWithUnknownFlagFails(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"admin", "create", "alice", "--bogus"}, "/app", io.Discard, &stderr, runner)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("stderr = %q, want unknown flag message", stderr.String())
	}
	if !reflect.DeepEqual(runner.command, recordedCommand{}) {
		t.Fatalf("runner was invoked = %#v, want no invocation on flag error", runner.command)
	}
}

func TestAdminGrantAndRevokeStaffAndSuperuserVerbsRunAppWithMatchingFlag(t *testing.T) {
	for _, tc := range []struct {
		verb string
		flag string
	}{
		{"grant-staff", "-tango-admin-grant-staff"},
		{"revoke-staff", "-tango-admin-revoke-staff"},
		{"grant-superuser", "-tango-admin-grant-superuser"},
		{"revoke-superuser", "-tango-admin-revoke-superuser"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			runner := &recordingRunner{}
			code := Run(context.Background(), []string{"admin", tc.verb, "alice"}, "/app", io.Discard, io.Discard, runner)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			want := recordedCommand{
				dir:  "/app",
				name: "go",
				args: []string{"run", ".", tc.flag + "=alice"},
			}
			if !reflect.DeepEqual(runner.command, want) {
				t.Fatalf("command = %#v, want %#v", runner.command, want)
			}
		})
	}
}

func TestAdminGrantStaffWithUnexpectedExtraArgumentFails(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"admin", "grant-staff", "alice", "--no-staff"}, "/app", io.Discard, &stderr, runner)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Fatalf("stderr = %q, want unexpected arguments message", stderr.String())
	}
	if !reflect.DeepEqual(runner.command, recordedCommand{}) {
		t.Fatalf("runner was invoked = %#v, want no invocation on argument error", runner.command)
	}
}

func TestAdminUnknownSubcommandFails(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"admin", "bogus", "alice"}, "/app", io.Discard, &stderr, runner)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("stderr = %q, want unknown subcommand", stderr.String())
	}
}

func TestAdminMissingUsernameFails(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"admin", "create"}, "/app", io.Discard, &stderr, runner)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr = %q, want usage message", stderr.String())
	}
}

func TestTuiCommandDispatchesToStatusFetch(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"tui"}, "/app", io.Discard, &stderr, runner)

	// isInteractiveTerminal() is false in the test environment, so tui()
	// falls back to a status fetch; an empty stdout payload fails to decode
	// as JSON, which is enough to prove Run's "tui" case reaches tui() with
	// the right arguments without depending on a real terminal.
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (empty status payload fails to decode), stderr: %s", code, stderr.String())
	}
	want := recordedCommand{
		dir:  "/app",
		name: "go",
		args: []string{"run", ".", "-tango-status"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("command = %#v, want %#v", runner.command, want)
	}
}

func TestRunWithNilRunnerDefaultsToExecRunner(t *testing.T) {
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"shell"}, "/app", io.Discard, &stderr, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Yaegi") {
		t.Fatalf("stderr = %q, want Yaegi direction (nil runner must not panic)", stderr.String())
	}
}

func TestRunnerErrorReturnsFailure(t *testing.T) {
	var stderr bytes.Buffer
	runner := &recordingRunner{err: errors.New("go missing")}

	code := Run(context.Background(), []string{"run"}, "/app", io.Discard, &stderr, runner)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "go missing") {
		t.Fatalf("stderr = %q, want runner error", stderr.String())
	}
}

// exitErrorRunner returns a real *exec.ExitError (from actually running a
// failing command), so runGo's errors.As(err, &exitError) branch — distinct
// from its generic-error fallback, already covered by
// TestRunnerErrorReturnsFailure — gets exercised with the real type it's
// written to detect.
type exitErrorRunner struct{}

func (exitErrorRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "false")
	return cmd.Run()
}

func TestRunnerExitErrorPropagatesRealExitCode(t *testing.T) {
	code := Run(context.Background(), []string{"run"}, "/app", io.Discard, io.Discard, exitErrorRunner{})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (the real exit code of `false`)", code)
	}
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
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

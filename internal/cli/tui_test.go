package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

// jsonStdoutRunner records every command it's asked to run and writes a
// fixed JSON payload to stdout each time, simulating an app's main.go
// handling -tango-status.
type jsonStdoutRunner struct {
	commands []recordedCommand
	payload  []byte
	err      error
}

func (r *jsonStdoutRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	r.commands = append(r.commands, recordedCommand{
		dir:  dir,
		name: name,
		args: append([]string(nil), args...),
	})
	if r.err != nil {
		return r.err
	}
	_, err := stdout.Write(r.payload)
	return err
}

func TestMenuItemsOrder(t *testing.T) {
	want := []tuiMenuItem{menuRunServer, menuApplyMigrations, menuRollbackLast}
	items := menuItems()
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, item := range items {
		if item != want[i] {
			t.Fatalf("item[%d] = %v, want %v", i, item, want[i])
		}
	}
}

func TestMenuItemAvailabilityFollowsProjectStatus(t *testing.T) {
	ready := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 2, MigrationsApplied: 1, MigrationsPending: 1}
	with := func(change func(*tango.ProjectStatus)) tango.ProjectStatus {
		s := ready
		change(&s)
		return s
	}
	const incomplete = "unavailable: project status is incomplete"
	tests := []struct {
		name       string
		item       tuiMenuItem
		status     tango.ProjectStatus
		wantOK     bool
		wantReason string
	}{
		{"run server is always available", menuRunServer, tango.ProjectStatus{}, true, ""},
		{"apply with something pending", menuApplyMigrations, ready, true, ""},
		{"apply with nothing pending", menuApplyMigrations, with(func(s *tango.ProjectStatus) { s.MigrationsPending = 0 }), false, "nothing to apply"},
		{"apply when registration failed", menuApplyMigrations, with(func(s *tango.ProjectStatus) { s.RegistrationOK = false }), false, incomplete},
		{"apply when the database is unreachable", menuApplyMigrations, with(func(s *tango.ProjectStatus) { s.DatabaseReachable = false }), false, incomplete},
		{"rollback with something applied", menuRollbackLast, ready, true, ""},
		{"rollback with nothing applied", menuRollbackLast, with(func(s *tango.ProjectStatus) { s.MigrationsApplied = 0 }), false, "nothing to roll back"},
		{"rollback when registration failed", menuRollbackLast, with(func(s *tango.ProjectStatus) { s.RegistrationOK = false }), false, incomplete},
		{"rollback when the database is unreachable", menuRollbackLast, with(func(s *tango.ProjectStatus) { s.DatabaseReachable = false }), false, incomplete},
		{"incomplete status wins over nothing to apply", menuApplyMigrations, with(func(s *tango.ProjectStatus) { s.DatabaseReachable = false; s.MigrationsPending = 0 }), false, incomplete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := tt.item.availability(tt.status)
			if ok != tt.wantOK || reason != tt.wantReason {
				t.Fatalf("availability = (%v, %q), want (%v, %q)", ok, reason, tt.wantOK, tt.wantReason)
			}
		})
	}
}

func TestConfirmationsSayWhatTheyWillDo(t *testing.T) {
	cases := map[tuiMenuItem]string{
		menuApplyMigrations: "Apply pending migrations? (y/n)",
		menuRollbackLast:    "Roll back the most recently applied migration? (y/n)",
	}
	for item, want := range cases {
		if got := item.confirmation(); got != want {
			t.Fatalf("%v.confirmation() = %q, want %q", item, got, want)
		}
	}
}

func TestTuiMenuItemLabelUnknownValueFallsBack(t *testing.T) {
	var unknown tuiMenuItem = 99
	if got := unknown.label(); got != "unknown" {
		t.Fatalf("label() = %q, want %q for an out-of-range menu item", got, "unknown")
	}
}

func TestExecRunnerRunsCommandInDirWithWiredOutput(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr strings.Builder

	err := ExecRunner{}.Run(context.Background(), dir, "pwd", nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run: %v, stderr: %s", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != dir {
		// macOS /tmp is often a symlink to /private/tmp; pwd -P style
		// mismatches would show up here too, so resolve both sides.
		resolvedDir, _ := filepath.EvalSymlinks(dir)
		resolvedGot, _ := filepath.EvalSymlinks(got)
		if resolvedGot != resolvedDir {
			t.Fatalf("pwd output = %q, want %q (dir wiring)", got, dir)
		}
	}
}

func TestExecRunnerReturnsErrorForMissingCommand(t *testing.T) {
	dir := t.TempDir()
	err := ExecRunner{}.Run(context.Background(), dir, "tango-cli-test-definitely-not-a-real-binary", nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("Run error = nil, want an error for a nonexistent command")
	}
}

func TestMenuItemsRequireConfirmationOnlyForMigrationActions(t *testing.T) {
	cases := map[tuiMenuItem]bool{
		menuRunServer:       false,
		menuApplyMigrations: true,
		menuRollbackLast:    true,
	}
	for item, want := range cases {
		if got := item.requiresConfirmation(); got != want {
			t.Fatalf("%v.requiresConfirmation() = %v, want %v", item, got, want)
		}
	}
}

func TestTUIFallsBackToPlainTextInNonInteractiveEnvironment(t *testing.T) {
	dir := t.TempDir()
	status := tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationsTotal:   2,
		MigrationsApplied: 1,
		MigrationsPending: 1,
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	runner := &jsonStdoutRunner{payload: encoded}

	var stdout, stderr strings.Builder
	code := tui(context.Background(), runner, dir, &stdout, &stderr, func() bool { return false }, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want exactly one -tango-status call", runner.commands)
	}
	want := recordedCommand{dir: dir, name: "go", args: []string{"run", ".", "-tango-status"}}
	got := runner.commands[0]
	if got.dir != want.dir || got.name != want.name || strings.Join(got.args, " ") != strings.Join(want.args, " ") {
		t.Fatalf("command = %+v, want %+v", got, want)
	}

	out := stdout.String()
	for _, want := range []string{"registration: ok", "database: reachable", "2 total, 1 applied, 1 pending"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, out)
		}
	}
}

func TestTUIFetchStatusFailurePropagatesError(t *testing.T) {
	dir := t.TempDir()
	runner := &jsonStdoutRunner{err: errors.New("go run failed")}

	var stdout, stderr strings.Builder
	code := tui(context.Background(), runner, dir, &stdout, &stderr, func() bool { return true }, nil)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "go run failed") {
		t.Fatalf("stderr = %q, want it to contain the underlying error", stderr.String())
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want exactly one attempted -tango-status call, no interactive attempt", runner.commands)
	}
}

func TestTUIExplainsAStatusFailureAndKeepsTheRawError(t *testing.T) {
	tests := []struct {
		name   string
		runner Runner
		raw    string
	}{
		{"the project fails to run", &jsonStdoutRunner{err: errors.New("go run failed")}, "tango tui: go run failed"},
		{"the entrypoint does not answer -tango-status", &jsonStdoutRunner{payload: []byte("listening on :8000\n")}, "tango tui: decode status:"},
	}
	for _, tt := range tests {
		for _, interactive := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, interactive=%v", tt.name, interactive), func(t *testing.T) {
				var stdout, stderr strings.Builder
				code := tui(context.Background(), tt.runner, t.TempDir(), &stdout, &stderr, func() bool { return interactive }, nil)
				if code != 1 {
					t.Fatalf("exit code = %d, want 1", code)
				}
				out := stderr.String()
				explanation := strings.Index(out, "tanGO could not load project status")
				raw := strings.Index(out, tt.raw)
				if explanation < 0 || raw < 0 || explanation > raw {
					t.Fatalf("stderr must explain first, then keep the raw error %q:\n%s", tt.raw, out)
				}
				for _, want := range []string{"tango check", "tango.DispatchFlags"} {
					if !strings.Contains(out, want) {
						t.Fatalf("stderr does not mention %q:\n%s", want, out)
					}
				}
				if stdout.Len() != 0 {
					t.Fatalf("stdout = %q, want nothing", stdout.String())
				}
			})
		}
	}
}

func TestPerformActionRunServerInvokesSameCommandAsTangoRun(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	performAction(context.Background(), runner, dir, io.Discard, io.Discard, menuRunServer, false)

	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want 1", runner.commands)
	}
	want := recordedCommand{dir: dir, name: "go", args: []string{"run", "."}}
	got := runner.commands[0]
	if got.dir != want.dir || got.name != want.name || strings.Join(got.args, " ") != strings.Join(want.args, " ") {
		t.Fatalf("command = %+v, want %+v", got, want)
	}
}

func TestPerformActionApplyMigrationsRunsOnlyWhenConfirmed(t *testing.T) {
	dir := t.TempDir()

	declined := &multiRecordingRunner{}
	performAction(context.Background(), declined, dir, io.Discard, io.Discard, menuApplyMigrations, false)
	if len(declined.commands) != 0 {
		t.Fatalf("declined commands = %+v, want none", declined.commands)
	}

	confirmed := &multiRecordingRunner{}
	performAction(context.Background(), confirmed, dir, io.Discard, io.Discard, menuApplyMigrations, true)
	if len(confirmed.commands) != 1 {
		t.Fatalf("confirmed commands = %+v, want 1", confirmed.commands)
	}
	want := recordedCommand{dir: dir, name: "go", args: []string{"run", ".", "-migrate"}}
	got := confirmed.commands[0]
	if got.dir != want.dir || got.name != want.name || strings.Join(got.args, " ") != strings.Join(want.args, " ") {
		t.Fatalf("command = %+v, want %+v", got, want)
	}
}

func TestPerformActionRollbackRunsOnlyWhenConfirmed(t *testing.T) {
	dir := t.TempDir()

	declined := &multiRecordingRunner{}
	performAction(context.Background(), declined, dir, io.Discard, io.Discard, menuRollbackLast, false)
	if len(declined.commands) != 0 {
		t.Fatalf("declined commands = %+v, want none", declined.commands)
	}

	confirmed := &multiRecordingRunner{}
	performAction(context.Background(), confirmed, dir, io.Discard, io.Discard, menuRollbackLast, true)
	if len(confirmed.commands) != 1 {
		t.Fatalf("confirmed commands = %+v, want 1", confirmed.commands)
	}
	want := recordedCommand{dir: dir, name: "go", args: []string{"run", ".", "-migrate", "-down"}}
	got := confirmed.commands[0]
	if got.dir != want.dir || got.name != want.name || strings.Join(got.args, " ") != strings.Join(want.args, " ") {
		t.Fatalf("command = %+v, want %+v", got, want)
	}
}

func TestAdvanceDashboardQuitWithoutPerformingStopsWithoutRunningAnything(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	final := dashboardModel{performed: false}
	next, _, done, code := advanceDashboard(context.Background(), runner, dir, io.Discard, io.Discard, final)

	if !done {
		t.Fatal("done = false, want true — quitting without a selection must stop the loop")
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if next != (tango.ProjectStatus{}) {
		t.Fatalf("next = %+v, want zero value", next)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("commands = %+v, want none run", runner.commands)
	}
}

func TestAdvanceDashboardRefetchesStatusAfterConfirmedMigrationAction(t *testing.T) {
	dir := t.TempDir()
	refreshedStatus := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 1, MigrationsApplied: 1}
	encoded, err := json.Marshal(refreshedStatus)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	runner := &jsonStdoutRunner{payload: encoded}

	final := dashboardModel{performed: true, confirmed: true, action: menuApplyMigrations}
	next, _, done, code := advanceDashboard(context.Background(), runner, dir, io.Discard, io.Discard, final)

	if done {
		t.Fatal("done = true, want false — the dashboard should loop with the refreshed status")
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if next != refreshedStatus {
		t.Fatalf("next = %+v, want %+v", next, refreshedStatus)
	}

	if len(runner.commands) != 2 {
		t.Fatalf("commands = %+v, want 2 (apply, then re-fetch status)", runner.commands)
	}
	wantApply := recordedCommand{dir: dir, name: "go", args: []string{"run", ".", "-migrate"}}
	if got := runner.commands[0]; got.dir != wantApply.dir || got.name != wantApply.name || strings.Join(got.args, " ") != strings.Join(wantApply.args, " ") {
		t.Fatalf("commands[0] = %+v, want %+v", got, wantApply)
	}
	wantStatus := recordedCommand{dir: dir, name: "go", args: []string{"run", ".", "-tango-status"}}
	if got := runner.commands[1]; got.dir != wantStatus.dir || got.name != wantStatus.name || strings.Join(got.args, " ") != strings.Join(wantStatus.args, " ") {
		t.Fatalf("commands[1] = %+v, want %+v", got, wantStatus)
	}
}

func TestAdvanceDashboardDoesNotRefetchStatusForRunServer(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	final := dashboardModel{performed: true, action: menuRunServer}
	_, _, done, _ := advanceDashboard(context.Background(), runner, dir, io.Discard, io.Discard, final)

	if !done {
		t.Fatal("done = false, want true — run server exits the dashboard loop")
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want exactly 1 (the run-server command, no status re-fetch)", runner.commands)
	}
}

func TestAdvanceDashboardStatusRefetchFailureAfterASuccessfulActionStopsWithOne(t *testing.T) {
	runner := funcRunner(func(args []string, stdout, stderr io.Writer) error {
		if isStatusCommand(args) {
			return errors.New("go run failed")
		}
		return nil
	})

	final := dashboardModel{performed: true, confirmed: true, action: menuApplyMigrations}
	_, _, done, code := advanceDashboard(context.Background(), runner, t.TempDir(), io.Discard, io.Discard, final)

	if !done || code != 1 {
		t.Fatalf("done = %v, code = %d, want true, 1 — nothing failed to show, so a status that cannot load ends the session", done, code)
	}
}

func TestAdvanceDashboardDeclinedConfirmationStillRefetchesStatus(t *testing.T) {
	dir := t.TempDir()
	refreshedStatus := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true}
	encoded, err := json.Marshal(refreshedStatus)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	runner := &jsonStdoutRunner{payload: encoded}

	final := dashboardModel{performed: true, confirmed: false, action: menuApplyMigrations}
	next, _, done, code := advanceDashboard(context.Background(), runner, dir, io.Discard, io.Discard, final)

	if done {
		t.Fatal("done = true, want false — a declined confirmation still loops with a refreshed status attempt")
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if next != refreshedStatus {
		t.Fatalf("next = %+v, want %+v", next, refreshedStatus)
	}
	// performAction is a no-op for a declined confirmation, so only the
	// status re-fetch command runs.
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want exactly 1 (status re-fetch only, no migration command)", runner.commands)
	}
}

// funcRunner runs fn for every command, so a test can fail one command and
// answer another.
type funcRunner func(args []string, stdout, stderr io.Writer) error

func (f funcRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	return f(args, stdout, stderr)
}

// exitError is the *exec.ExitError a command that exited with code gives.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an exit error, got %v", err)
	}
	return err
}

func isStatusCommand(args []string) bool { return args[len(args)-1] == "-tango-status" }

func TestAdvanceDashboardKeepsGoingWhenAMigrationActionFails(t *testing.T) {
	refreshed := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 1, MigrationsPending: 1}
	encoded, err := json.Marshal(refreshed)
	if err != nil {
		t.Fatal(err)
	}
	runner := funcRunner(func(args []string, stdout, stderr io.Writer) error {
		if isStatusCommand(args) {
			_, err := stdout.Write(encoded)
			return err
		}
		fmt.Fprintln(stderr, "migration 0002_add_title: duplicate column")
		fmt.Fprintln(stderr, "exit status 3")
		return exitError(t, 3)
	})

	final := dashboardModel{performed: true, confirmed: true, action: menuApplyMigrations}
	next, notice, done, code := advanceDashboard(context.Background(), runner, t.TempDir(), io.Discard, io.Discard, final)

	if done || code != 0 {
		t.Fatalf("done = %v, code = %d; a failed action must return to the menu", done, code)
	}
	if next != refreshed {
		t.Fatalf("next = %+v, want the refreshed status %+v", next, refreshed)
	}
	for _, want := range []string{"Last action failed: Apply pending migrations", "exit code 3", "migration 0002_add_title: duplicate column"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice %q does not contain %q", notice, want)
		}
	}
}

func TestAdvanceDashboardReportsBothErrorsWhenTheRefreshAlsoFails(t *testing.T) {
	runner := funcRunner(func(args []string, stdout, stderr io.Writer) error {
		if isStatusCommand(args) {
			return errors.New("status unavailable")
		}
		fmt.Fprintln(stderr, "boom")
		return errors.New("migrate failed")
	})
	old := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsApplied: 1}

	final := dashboardModel{status: old, performed: true, confirmed: true, action: menuRollbackLast}
	next, notice, done, code := advanceDashboard(context.Background(), runner, t.TempDir(), io.Discard, io.Discard, final)

	if done || code != 0 {
		t.Fatalf("done = %v, code = %d; the dashboard must stay up", done, code)
	}
	if next != old {
		t.Fatalf("next = %+v, want the status from before the action %+v", next, old)
	}
	action := strings.Index(notice, "Last action failed: Roll back the latest migration")
	refresh := strings.Index(notice, "Status refresh also failed: status unavailable")
	if action < 0 || refresh < 0 || action > refresh {
		t.Fatalf("notice must give the action error first, then the refresh error:\n%s", notice)
	}
}

func TestAdvanceDashboardRunServerFailureStillExitsWithItsCode(t *testing.T) {
	runner := &multiRecordingRunner{err: errors.New("server failed")}
	final := dashboardModel{performed: true, action: menuRunServer}
	_, notice, done, code := advanceDashboard(context.Background(), runner, t.TempDir(), io.Discard, io.Discard, final)
	if !done || code != 1 || notice != "" {
		t.Fatalf("done = %v, code = %d, notice = %q; a failing server keeps ending the TUI with its code", done, code, notice)
	}
}

func TestLastLineWriterKeepsTheLastNonEmptyLine(t *testing.T) {
	var out strings.Builder
	w := &lastLineWriter{w: &out}
	fmt.Fprint(w, "first\nsecond\n\n")
	if got := w.last(); got != "second" {
		t.Fatalf("last() = %q, want second", got)
	}
	fmt.Fprint(w, "third, no newline")
	if got := w.last(); got != "third, no newline" {
		t.Fatalf("last() = %q, want the unterminated line", got)
	}
	if out.String() != "first\nsecond\n\nthird, no newline" {
		t.Fatalf("output was altered: %q", out.String())
	}
}

func TestPrintStatusPlainRendersFailureStates(t *testing.T) {
	var out strings.Builder
	printStatusPlain(&out, tango.ProjectStatus{
		RegistrationOK:    false,
		RegistrationError: "boom",
		DatabaseReachable: false,
		DatabaseError:     "no db",
	})

	got := out.String()
	for _, want := range []string{"registration: FAILED (boom)", "database: UNREACHABLE (no db)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output does not contain %q:\n%s", want, got)
		}
	}
}

// stopSignalsAs replaces the stop-signal watcher for one test: received says
// whether the "user pressed Ctrl-C" while the server ran.
func stopSignalsAs(t *testing.T, received bool) {
	t.Helper()
	old := watchStopSignals
	watchStopSignals = func() func() bool { return func() bool { return received } }
	t.Cleanup(func() { watchStopSignals = old })
}

func TestRunServerStoppedByTheUserEndsQuietlyWithZero(t *testing.T) {
	// `go run` exits 1 after an interrupt even when the server shut down
	// gracefully, and is killed by the signal (ExitCode -1) on SIGTERM.
	for _, serverErr := range []error{nil, exitError(t, 1), killedBySignal(t)} {
		stopSignalsAs(t, true)
		var stdout strings.Builder
		runner := &multiRecordingRunner{err: serverErr}
		final := dashboardModel{performed: true, action: menuRunServer}
		_, notice, done, code := advanceDashboard(context.Background(), runner, t.TempDir(), &stdout, io.Discard, final)
		if !done || code != 0 || notice != "" {
			t.Fatalf("server error %v: done = %v, code = %d; a stop by the user exits 0", serverErr, done, code)
		}
		if !strings.Contains(stdout.String(), "server stopped") {
			t.Fatalf("server error %v: stdout = %q, want a 'server stopped' line", serverErr, stdout.String())
		}
	}
}

func TestRunServerFailureKeepsItsExitCode(t *testing.T) {
	tests := []struct {
		name     string
		received bool
		err      error
		want     int
	}{
		{"crashed with no signal", false, exitError(t, 1), 1},
		{"exit code 2 with no signal", false, exitError(t, 2), 2},
		{"exited by itself with zero", false, nil, 0},
		{"a real failure code after the signal", true, exitError(t, 2), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopSignalsAs(t, tt.received)
			var stdout strings.Builder
			runner := &multiRecordingRunner{err: tt.err}
			final := dashboardModel{performed: true, action: menuRunServer}
			_, _, done, code := advanceDashboard(context.Background(), runner, t.TempDir(), &stdout, io.Discard, final)
			if !done || code != tt.want {
				t.Fatalf("done = %v, code = %d, want true, %d", done, code, tt.want)
			}
			if strings.Contains(stdout.String(), "server stopped") {
				t.Fatalf("stdout = %q; only a stop by the user prints 'server stopped'", stdout.String())
			}
		})
	}
}

// killedBySignal is the *exec.ExitError of a process the kernel killed with
// a signal; its ExitCode is -1.
func killedBySignal(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
		t.Fatalf("expected a signal-killed exit error, got %v", err)
	}
	return err
}

func TestUnknownMenuItemsAreHarmless(t *testing.T) {
	var unknown tuiMenuItem = 99
	if got := unknown.confirmation(); got != "unknown? (y/n)" {
		t.Fatalf("confirmation() = %q", got)
	}
	runner := &multiRecordingRunner{}
	if code := performAction(context.Background(), runner, t.TempDir(), io.Discard, io.Discard, unknown, true); code != 0 || len(runner.commands) != 0 {
		t.Fatalf("code = %d, commands = %v; an unknown item must run nothing", code, runner.commands)
	}
}

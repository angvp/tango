package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestMenuItemsOrderAndAvailability(t *testing.T) {
	items := menuItems()
	want := []tuiMenuItem{menuRunServer, menuApplyMigrations, menuRollbackLast, menuShell}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, item := range items {
		if item != want[i] {
			t.Fatalf("item[%d] = %v, want %v", i, item, want[i])
		}
	}

	if !menuRunServer.available() {
		t.Fatal("menuRunServer.available() = false, want true")
	}
	if !menuApplyMigrations.available() {
		t.Fatal("menuApplyMigrations.available() = false, want true")
	}
	if !menuRollbackLast.available() {
		t.Fatal("menuRollbackLast.available() = false, want true")
	}
	if menuShell.available() {
		t.Fatal("menuShell.available() = true, want false (tango shell isn't implemented yet)")
	}
}

func TestMenuItemsRequireConfirmationOnlyForMigrationActions(t *testing.T) {
	cases := map[tuiMenuItem]bool{
		menuRunServer:       false,
		menuApplyMigrations: true,
		menuRollbackLast:    true,
		menuShell:           false,
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
	code := tui(context.Background(), runner, dir, &stdout, &stderr, func() bool { return false })
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
	code := tui(context.Background(), runner, dir, &stdout, &stderr, func() bool { return true })
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

func TestAdvanceDashboardRefetchesStatusAfterConfirmedMigrationAction(t *testing.T) {
	dir := t.TempDir()
	refreshedStatus := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 1, MigrationsApplied: 1}
	encoded, err := json.Marshal(refreshedStatus)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	runner := &jsonStdoutRunner{payload: encoded}

	final := dashboardModel{performed: true, confirmed: true, action: menuApplyMigrations}
	next, done, code := advanceDashboard(context.Background(), runner, dir, io.Discard, io.Discard, final)

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
	_, done, _ := advanceDashboard(context.Background(), runner, dir, io.Discard, io.Discard, final)

	if !done {
		t.Fatal("done = false, want true — run server exits the dashboard loop")
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %+v, want exactly 1 (the run-server command, no status re-fetch)", runner.commands)
	}
}

func TestPerformActionShellIsInformationalOnly(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	performAction(context.Background(), runner, dir, io.Discard, io.Discard, menuShell, true)

	if len(runner.commands) != 0 {
		t.Fatalf("commands = %+v, want none — menuShell must not run anything", runner.commands)
	}
}

package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/angvp/tango"
)

// Keys as a terminal sends them.
const (
	keyDown  = "\x1b[B"
	keyEnter = "\r"
)

// keyReader hands out one key per Read, as a terminal does. Bubble Tea
// merges typed characters that arrive in one read ("nq") into a single key,
// so a script cannot be one string.
type keyReader struct{ keys []string }

func (r *keyReader) Read(p []byte) (int, error) {
	if len(r.keys) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.keys[0])
	r.keys = r.keys[1:]
	return n, nil
}

// session is the keys one dashboard session receives.
func session(keys ...string) []string { return keys }

// scriptedSessions returns programOptions that give each dashboard session
// (runDashboard starts a new Bubble Tea program after every action) its own
// keystrokes, so one session cannot swallow the next one's input. Output of
// every session is appended to out.
func scriptedSessions(out *strings.Builder, sessions ...[]string) func() []tea.ProgramOption {
	next := 0
	return func() []tea.ProgramOption {
		keys := []string{"q"} // a session beyond the script just quits
		if next < len(sessions) {
			keys = sessions[next]
		}
		next++
		return []tea.ProgramOption{tea.WithInput(&keyReader{keys: keys}), tea.WithOutput(out)}
	}
}

func statusRunner(t *testing.T, status tango.ProjectStatus) *jsonStdoutRunner {
	t.Helper()
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	return &jsonStdoutRunner{payload: encoded}
}

func commandLines(runner *jsonStdoutRunner) []string {
	var lines []string
	for _, c := range runner.commands {
		lines = append(lines, c.name+" "+strings.Join(c.args, " "))
	}
	return lines
}

func pendingStatus() tango.ProjectStatus {
	return tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 2, MigrationsApplied: 1, MigrationsPending: 1}
}

func TestEventLoopQuitRunsNothing(t *testing.T) {
	var screen strings.Builder
	runner := statusRunner(t, pendingStatus())
	code := runDashboard(context.Background(), runner, t.TempDir(), &strings.Builder{}, &strings.Builder{}, pendingStatus(), scriptedSessions(&screen, session("q")))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("commands = %v, want none", commandLines(runner))
	}
	if !strings.Contains(screen.String(), "tanGO project status") {
		t.Fatalf("the dashboard never rendered:\n%s", screen.String())
	}
}

func TestEventLoopDecliningAnActionRunsNothingAndReturnsToTheMenu(t *testing.T) {
	var screen strings.Builder
	runner := statusRunner(t, pendingStatus())
	// Down to "Apply pending migrations", Enter, "n" declines, then quit.
	code := runDashboard(context.Background(), runner, t.TempDir(), &strings.Builder{}, &strings.Builder{}, pendingStatus(), scriptedSessions(&screen, session(keyDown, keyEnter, "n", "q")))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("a declined action ran %v", commandLines(runner))
	}

	// Back on the menu, with the cursor where it was: Enter and "y" now runs it.
	runner = statusRunner(t, pendingStatus())
	runDashboard(context.Background(), runner, t.TempDir(), &strings.Builder{}, &strings.Builder{}, pendingStatus(), scriptedSessions(&screen, session(keyDown, keyEnter, "n", keyEnter, "y"), session("q")))
	if got := commandLines(runner); len(got) == 0 || got[0] != "go run . -migrate" {
		t.Fatalf("commands = %v, want the apply to run after a decline and a second confirmation", got)
	}
}

func TestEventLoopConfirmedActionRunsThenRefreshesStatusAndReturnsToTheMenu(t *testing.T) {
	var screen strings.Builder
	runner := statusRunner(t, tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 2, MigrationsApplied: 2})
	// Session 1 applies; session 2 (refreshed status) quits.
	code := runDashboard(context.Background(), runner, t.TempDir(), &strings.Builder{}, &strings.Builder{}, pendingStatus(), scriptedSessions(&screen, session(keyDown, keyEnter, "y"), session("q")))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := []string{"go run . -migrate", "go run . -tango-status"}
	if got := commandLines(runner); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	if !strings.Contains(screen.String(), "2 total, 2 applied, 0 pending") {
		t.Fatalf("the refreshed status was never shown:\n%s", screen.String())
	}
}

func TestTUIStartsTheDashboardOnlyInAnInteractiveTerminal(t *testing.T) {
	for _, tt := range []struct {
		name        string
		interactive bool
		wantMenu    bool
		wantPlain   bool
	}{
		{"interactive", true, true, false},
		{"not interactive", false, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var screen, stdout strings.Builder
			runner := statusRunner(t, pendingStatus())
			code := tui(context.Background(), runner, t.TempDir(), &stdout, &strings.Builder{}, func() bool { return tt.interactive }, scriptedSessions(&screen, session("q")))
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if got := strings.Contains(screen.String(), "Run server"); got != tt.wantMenu {
				t.Errorf("menu rendered = %v, want %v", got, tt.wantMenu)
			}
			if got := strings.Contains(stdout.String(), "actions available via:"); got != tt.wantPlain {
				t.Errorf("plain status printed = %v, want %v", got, tt.wantPlain)
			}
		})
	}
}

func TestEventLoopSelectingAGreyedItemDoesNothing(t *testing.T) {
	var screen strings.Builder
	status := pendingStatus()
	status.MigrationsPending = 0 // nothing to apply
	runner := statusRunner(t, status)
	// Down to the greyed "Apply", Enter, then "y": no prompt, so nothing runs.
	code := runDashboard(context.Background(), runner, t.TempDir(), &strings.Builder{}, &strings.Builder{}, status, scriptedSessions(&screen, session(keyDown, keyEnter, "y", "q")))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("a greyed item ran %v", commandLines(runner))
	}
}

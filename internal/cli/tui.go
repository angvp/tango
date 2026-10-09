package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/angvp/tango"
)

// tuiMenuItem identifies one selectable action on the `tango tui` dashboard.
type tuiMenuItem int

const (
	menuRunServer tuiMenuItem = iota
	menuApplyMigrations
	menuRollbackLast
)

// menuItems returns the dashboard's menu in display order.
func menuItems() []tuiMenuItem {
	return []tuiMenuItem{menuRunServer, menuApplyMigrations, menuRollbackLast}
}

func (m tuiMenuItem) label() string {
	switch m {
	case menuRunServer:
		return "Run server"
	case menuApplyMigrations:
		return "Apply pending migrations"
	case menuRollbackLast:
		return "Roll back the latest migration"
	default:
		return "unknown"
	}
}

// confirmation is the yes/no question asked before m runs, in plain words.
// ProjectStatus names no migrations, so none is named here.
func (m tuiMenuItem) confirmation() string {
	switch m {
	case menuApplyMigrations:
		return "Apply pending migrations? (y/n)"
	case menuRollbackLast:
		return "Roll back the most recently applied migration? (y/n)"
	default:
		return m.label() + "? (y/n)"
	}
}

// availability reports whether selecting m can do anything given status,
// and when it cannot, why. It uses only what ProjectStatus already carries:
// migration actions need registration and the database to be in order, and
// something to apply or roll back.
func (m tuiMenuItem) availability(status tango.ProjectStatus) (ok bool, reason string) {
	switch m {
	case menuApplyMigrations:
		if !statusComplete(status) {
			return false, reasonStatusIncomplete
		}
		if status.MigrationsPending == 0 {
			return false, "nothing to apply"
		}
	case menuRollbackLast:
		if !statusComplete(status) {
			return false, reasonStatusIncomplete
		}
		if status.MigrationsApplied == 0 {
			return false, "nothing to roll back"
		}
	}
	return true, ""
}

const reasonStatusIncomplete = "unavailable: project status is incomplete"

// statusComplete reports whether registration and the database are in order,
// the precondition for any migration action.
func statusComplete(status tango.ProjectStatus) bool {
	return status.RegistrationOK && status.DatabaseReachable
}

// requiresConfirmation reports whether m is destructive enough to need an
// explicit yes/no prompt before it runs.
func (m tuiMenuItem) requiresConfirmation() bool {
	return m == menuApplyMigrations || m == menuRollbackLast
}

// isInteractiveTerminal reports whether both stdin and stdout are attached
// to a real terminal capable of running the dashboard. It is the one piece of
// the TUI no test drives: tui takes it as a parameter, and every test
// supplies its own answer.
var isInteractiveTerminal = func() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// tui implements `tango tui`: a read-only status dashboard with a menu of
// actions, falling back to a plain-text status print in non-interactive
// environments (CI, pipes, unsupported terminals).
//
// interactive and programOptions are the TUI's two seams: interactive says
// whether a terminal is attached, and programOptions (nil in production)
// supplies each Bubble Tea program's options, so a test can script the keys.
func tui(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, interactive func() bool, programOptions func() []tea.ProgramOption) int {
	// The project's own stderr is held back so that, on failure, the
	// explanation comes first and the project's words follow it unchanged.
	var projectStderr bytes.Buffer
	status, err := fetchStatus(ctx, runner, dir, &projectStderr)
	if err != nil {
		fmt.Fprint(stderr, statusFailureHelp)
		stderr.Write(projectStderr.Bytes())
		fmt.Fprintf(stderr, "tango tui: %v\n", err)
		return 1
	}
	stderr.Write(projectStderr.Bytes())

	if !interactive() {
		printStatusPlain(stdout, status)
		return 0
	}

	return runDashboard(ctx, runner, dir, stdout, stderr, status, programOptions)
}

// statusFailureHelp precedes the raw error when the project's status cannot
// be loaded; it never replaces it.
const statusFailureHelp = `tango tui: tanGO could not load project status.
  - Run "tango check" to diagnose registration and configuration.
  - Older project entrypoints may need tango.DispatchFlags, which answers
    -tango-status.
`

// fetchStatus shells `-tango-status`, the same convention `tango check`/
// `makemigrations` already use for `-check`/`-tango-dump-models`.
func fetchStatus(ctx context.Context, runner Runner, dir string, stderr io.Writer) (tango.ProjectStatus, error) {
	var out bytes.Buffer
	if err := runner.Run(ctx, dir, "go", []string{"run", ".", "-tango-status"}, &out, stderr); err != nil {
		return tango.ProjectStatus{}, err
	}
	var status tango.ProjectStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		return tango.ProjectStatus{}, fmt.Errorf("decode status: %w", err)
	}
	return status, nil
}

func printStatusPlain(w io.Writer, status tango.ProjectStatus) {
	fmt.Fprintln(w, "tanGO project status")
	if status.RegistrationOK {
		fmt.Fprintln(w, "  registration: ok")
	} else {
		fmt.Fprintf(w, "  registration: FAILED (%s)\n", status.RegistrationError)
	}
	if status.DatabaseReachable {
		fmt.Fprintln(w, "  database: reachable")
	} else {
		fmt.Fprintf(w, "  database: UNREACHABLE (%s)\n", status.DatabaseError)
	}
	fmt.Fprintf(w, "  migrations: %d total, %d applied, %d pending\n",
		status.MigrationsTotal, status.MigrationsApplied, status.MigrationsPending)
	fmt.Fprintln(w, "actions available via: tango run | tango migrate | tango migrate down")
}

// performAction executes item's effect via the exact same command path the
// equivalent non-interactive command already uses. Confirmation-gated items
// (menuApplyMigrations, menuRollbackLast) perform nothing unless confirmed
// is true.
func performAction(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, item tuiMenuItem, confirmed bool) int {
	switch item {
	case menuRunServer:
		return runGo(ctx, runner, dir, stdout, stderr, []string{"run", "."})
	case menuApplyMigrations:
		if !confirmed {
			return 0
		}
		return runGo(ctx, runner, dir, stdout, stderr, []string{"run", ".", "-migrate"})
	case menuRollbackLast:
		if !confirmed {
			return 0
		}
		return runGo(ctx, runner, dir, stdout, stderr, []string{"run", ".", "-migrate", "-down"})
	default:
		return 0
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/angvp/tango"
)

// tuiMenuItem identifies one selectable action on the `tango tui` dashboard.
type tuiMenuItem int

const (
	menuRunServer tuiMenuItem = iota
	menuApplyMigrations
	menuRollbackLast
	menuShell
)

// menuItems returns the dashboard's menu in display order.
func menuItems() []tuiMenuItem {
	return []tuiMenuItem{menuRunServer, menuApplyMigrations, menuRollbackLast, menuShell}
}

func (m tuiMenuItem) label() string {
	switch m {
	case menuRunServer:
		return "Run server"
	case menuApplyMigrations:
		return "Apply pending migrations"
	case menuRollbackLast:
		return "Roll back the latest migration"
	case menuShell:
		return "Shell (not implemented yet)"
	default:
		return "unknown"
	}
}

// available reports whether selecting m performs a real action. menuShell is
// listed for discoverability only, since tango shell isn't implemented yet.
func (m tuiMenuItem) available() bool {
	return m != menuShell
}

// requiresConfirmation reports whether m is destructive enough to need an
// explicit yes/no prompt before it runs.
func (m tuiMenuItem) requiresConfirmation() bool {
	return m == menuApplyMigrations || m == menuRollbackLast
}

// isInteractiveTerminal reports whether both stdin and stdout are attached
// to a real terminal capable of running the dashboard. Overridable in tests.
var isInteractiveTerminal = func() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// tui implements `tango tui`: a read-only status dashboard with a menu of
// actions, falling back to a plain-text status print in non-interactive
// environments (CI, pipes, unsupported terminals). See Milestone 8.3.
func tui(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, interactive func() bool) int {
	status, err := fetchStatus(ctx, runner, dir, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "tango tui: %v\n", err)
		return 1
	}

	if !interactive() {
		printStatusPlain(stdout, status)
		return 0
	}

	return runDashboard(ctx, runner, dir, stdout, stderr, status)
}

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

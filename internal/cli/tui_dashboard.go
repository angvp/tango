package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/angvp/tango"
)

var (
	tuiTitleStyle   = lipgloss.NewStyle().Bold(true)
	tuiCursorStyle  = lipgloss.NewStyle().Bold(true)
	tuiUnavailStyle = lipgloss.NewStyle().Faint(true)
	tuiConfirmStyle = lipgloss.NewStyle().Bold(true)
	tuiFooterStyle  = lipgloss.NewStyle().Faint(true)
)

// dashboardModel is the bubbletea model backing the interactive `tango tui`
// screen. Navigation and confirmation live here; actually running a command
// happens outside the event loop, in performAction (see tui.go), so the two
// concerns stay independently testable.
type dashboardModel struct {
	status  tango.ProjectStatus
	items   []tuiMenuItem
	cursor  int
	confirm bool

	quitting  bool
	performed bool
	confirmed bool
	action    tuiMenuItem
}

func newDashboardModel(status tango.ProjectStatus) dashboardModel {
	return dashboardModel{status: status, items: menuItems()}
}

func (m dashboardModel) Init() tea.Cmd { return nil }

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.confirm {
		switch keyMsg.String() {
		case "y", "Y":
			m.confirmed = true
			m.performed = true
			m.quitting = true
			return m, tea.Quit
		case "n", "N", "esc":
			m.confirm = false
			return m, nil
		}
		return m, nil
	}

	switch keyMsg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.cursor = m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.cursor = m.moveCursor(1)
		return m, nil
	case "enter":
		selected := m.items[m.cursor]
		if !selected.available() {
			return m, nil
		}
		m.action = selected
		if selected.requiresConfirmation() {
			m.confirm = true
			return m, nil
		}
		m.performed = true
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// moveCursor shifts the cursor by delta, wrapping around the menu.
func (m dashboardModel) moveCursor(delta int) int {
	next := (m.cursor + delta + len(m.items)) % len(m.items)
	return next
}

func (m dashboardModel) View() string {
	var b strings.Builder

	b.WriteString(tuiTitleStyle.Render("tanGO project status"))
	b.WriteString("\n")
	if m.status.RegistrationOK {
		b.WriteString("  registration: ok\n")
	} else {
		fmt.Fprintf(&b, "  registration: FAILED (%s)\n", m.status.RegistrationError)
	}
	if m.status.DatabaseReachable {
		b.WriteString("  database: reachable\n")
	} else {
		fmt.Fprintf(&b, "  database: UNREACHABLE (%s)\n", m.status.DatabaseError)
	}
	fmt.Fprintf(&b, "  migrations: %d total, %d applied, %d pending\n\n",
		m.status.MigrationsTotal, m.status.MigrationsApplied, m.status.MigrationsPending)

	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = tuiCursorStyle.Render("> ")
		}
		label := item.label()
		if !item.available() {
			label = tuiUnavailStyle.Render(label)
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, label)
	}

	if m.confirm {
		b.WriteString("\n")
		b.WriteString(tuiConfirmStyle.Render(fmt.Sprintf("%s? (y/n)", m.items[m.cursor].label())))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(tuiFooterStyle.Render("↑/↓ move · enter select · q quit"))
	b.WriteString("\n")

	return b.String()
}

// runDashboard runs the interactive dashboard, performing the selected
// action (outside bubbletea's event loop) and looping with a refreshed
// status after any state-changing action, per Milestone 8.3.
func runDashboard(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, status tango.ProjectStatus) int {
	for {
		program := tea.NewProgram(newDashboardModel(status))
		result, err := program.Run()
		if err != nil {
			fmt.Fprintf(stderr, "tango tui: %v\n", err)
			return 1
		}

		next, done, code := advanceDashboard(ctx, runner, dir, stdout, stderr, result.(dashboardModel))
		if done {
			return code
		}
		status = next
	}
}

// advanceDashboard applies one finished dashboardModel's selection: it
// performs the action (a no-op if it was a declined confirmation) and, for
// every action except menuRunServer, re-fetches status for the next loop
// iteration. done is true when runDashboard should stop and return code
// instead of looping with a refreshed status. This is a plain function over
// dashboardModel's exported-to-the-package fields specifically so it's
// testable without driving a real bubbletea event loop.
func advanceDashboard(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, final dashboardModel) (next tango.ProjectStatus, done bool, code int) {
	if !final.performed {
		return tango.ProjectStatus{}, true, 0
	}

	if final.action == menuRunServer {
		return tango.ProjectStatus{}, true, performAction(ctx, runner, dir, stdout, stderr, final.action, final.confirmed)
	}

	if code := performAction(ctx, runner, dir, stdout, stderr, final.action, final.confirmed); code != 0 {
		return tango.ProjectStatus{}, true, code
	}

	refreshed, err := fetchStatus(ctx, runner, dir, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "tango tui: %v\n", err)
		return tango.ProjectStatus{}, true, 1
	}
	return refreshed, false, 0
}

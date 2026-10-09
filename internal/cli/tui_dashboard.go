package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

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
	tuiNoticeStyle  = lipgloss.NewStyle().Bold(true)
)

// dashboardModel is the bubbletea model backing the interactive `tango tui`
// screen. Navigation and confirmation live here; actually running a command
// happens outside the event loop, in performAction (see tui.go), so the two
// concerns stay independently testable.
type dashboardModel struct {
	status  tango.ProjectStatus
	notice  string // the last failed action, shown until the next action ends
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
		if ok, _ := selected.availability(m.status); !ok {
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
		if ok, reason := item.availability(m.status); !ok {
			label = tuiUnavailStyle.Render(label + " (" + reason + ")")
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, label)
	}

	if m.confirm {
		b.WriteString("\n")
		b.WriteString(tuiConfirmStyle.Render(m.items[m.cursor].confirmation()))
		b.WriteString("\n")
	}

	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(tuiNoticeStyle.Render(m.notice))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(tuiFooterStyle.Render("↑/↓ move · enter select · q quit"))
	b.WriteString("\n")

	return b.String()
}

// runDashboard runs the interactive dashboard, performing the selected
// action (outside bubbletea's event loop) and looping with a refreshed
// status after any state-changing action. programOptions, when non-nil, is
// called once per session (each action ends a Bubble Tea program and starts
// the next) for that program's options.
func runDashboard(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, status tango.ProjectStatus, programOptions func() []tea.ProgramOption) int {
	notice := ""
	for {
		var options []tea.ProgramOption
		if programOptions != nil {
			options = programOptions()
		}
		model := newDashboardModel(status)
		model.notice = notice
		program := tea.NewProgram(model, options...)
		result, err := program.Run()
		if err != nil {
			fmt.Fprintf(stderr, "tango tui: %v\n", err)
			return 1
		}

		next, nextNotice, done, code := advanceDashboard(ctx, runner, dir, stdout, stderr, result.(dashboardModel))
		if done {
			return code
		}
		status, notice = next, nextNotice
	}
}

// advanceDashboard applies one finished dashboardModel's selection: it
// performs the action (a no-op if it was a declined confirmation) and, for
// every action except menuRunServer, re-fetches status for the next loop
// iteration. done is true when runDashboard should stop and return code
// instead of looping with a refreshed status.
//
// A migration action that fails does not end the session: notice says what
// failed, and the dashboard loops with refreshed status. If the refresh also
// fails, notice carries both errors and the dashboard keeps the old status.
// This is a plain function over dashboardModel's fields specifically so it's
// testable without driving a real bubbletea event loop.
func advanceDashboard(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, final dashboardModel) (next tango.ProjectStatus, notice string, done bool, code int) {
	if !final.performed {
		return tango.ProjectStatus{}, "", true, 0
	}

	if final.action == menuRunServer {
		return tango.ProjectStatus{}, "", true, runServer(ctx, runner, dir, stdout, stderr, final)
	}

	captured := &lastLineWriter{w: stderr}
	actionCode := performAction(ctx, runner, dir, stdout, captured, final.action, final.confirmed)
	if actionCode != 0 {
		notice = fmt.Sprintf("Last action failed: %s (exit code %d)", final.action.label(), actionCode)
		if line := captured.last(); line != "" {
			notice += ": " + line
		}
	}

	refreshed, err := fetchStatus(ctx, runner, dir, stderr)
	switch {
	case err == nil:
		return refreshed, notice, false, 0
	case actionCode != 0:
		return final.status, notice + "\nStatus refresh also failed: " + err.Error(), false, 0
	default:
		fmt.Fprintf(stderr, "tango tui: %v\n", err)
		return tango.ProjectStatus{}, "", true, 1
	}
}

var goRunExitLine = regexp.MustCompile(`^exit status \d+$`)

// lastLineWriter passes writes through to w and remembers the last
// non-empty line written, to quote in a failure notice. It skips the
// "exit status N" line `go run` appends, which says nothing about the cause.
type lastLineWriter struct {
	w    io.Writer
	line string
	buf  string
}

func (l *lastLineWriter) Write(p []byte) (int, error) {
	l.buf += string(p)
	lines := strings.Split(l.buf, "\n")
	l.buf = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !goRunExitLine.MatchString(trimmed) {
			l.line = trimmed
		}
	}
	return l.w.Write(p)
}

// last returns the last non-empty line, including a final unterminated one.
func (l *lastLineWriter) last() string {
	if trimmed := strings.TrimSpace(l.buf); trimmed != "" && !goRunExitLine.MatchString(trimmed) {
		return trimmed
	}
	return l.line
}

// watchStopSignals starts catching Ctrl-C and SIGTERM, and returns a function
// that stops catching them and reports whether one arrived. While a server
// runs from the TUI the terminal sends the signal to tango as well as to the
// server; catching it keeps tango alive to wait for the server and report a
// clean stop. A variable so tests can say whether the user pressed Ctrl-C.
var watchStopSignals = func() func() bool {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	return func() bool {
		signal.Stop(signals)
		select {
		case <-signals:
			return true
		default:
			return false
		}
	}
}

// runServer runs the project's server and returns the exit code for the TUI.
// A server the user stopped with Ctrl-C or SIGTERM is a normal stop: `go run`
// then exits 1 (even after a graceful shutdown) or is killed by the signal
// (exit code -1), so those codes, after a signal, print "server stopped" and
// give 0. Any other code, or any code with no signal, is a real failure and
// is kept.
func runServer(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, final dashboardModel) int {
	stopped := watchStopSignals()
	code := performAction(ctx, runner, dir, stdout, stderr, final.action, final.confirmed)
	if stopped() && (code == 0 || code == 1 || code == -1) {
		fmt.Fprintln(stdout, "server stopped")
		return 0
	}
	return code
}

package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/angvp/tango"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func typeKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func TestNewDashboardModelStartsAtFirstItemWithoutConfirmation(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})

	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	if m.confirm {
		t.Fatal("confirm = true, want false for a freshly created model")
	}
	if len(m.items) != len(menuItems()) {
		t.Fatalf("len(items) = %d, want %d", len(m.items), len(menuItems()))
	}
}

func TestDashboardUpdateIgnoresNonKeyMessages(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})

	next, cmd := m.Update(struct{}{})
	got := next.(dashboardModel)

	if got.cursor != m.cursor || got.confirm != m.confirm || got.performed != m.performed || got.quitting != m.quitting {
		t.Fatal("Update() changed the model in response to a non-key message")
	}
	if cmd != nil {
		t.Fatal("Update() returned a non-nil command for a non-key message")
	}
}

func TestDashboardUpdateMovesCursorDownAndWrapsAround(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})
	last := len(m.items) - 1

	for i := 0; i < last; i++ {
		next, _ := m.Update(typeKey(tea.KeyDown))
		m = next.(dashboardModel)
	}
	if m.cursor != last {
		t.Fatalf("cursor = %d, want %d after moving down to the last item", m.cursor, last)
	}

	next, _ := m.Update(runeKey('j'))
	m = next.(dashboardModel)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (wrapped around past the last item)", m.cursor)
	}
}

func TestDashboardUpdateMovesCursorUpAndWrapsAround(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})

	next, _ := m.Update(typeKey(tea.KeyUp))
	m = next.(dashboardModel)
	if m.cursor != len(m.items)-1 {
		t.Fatalf("cursor = %d, want %d (wrapped to the last item from the first)", m.cursor, len(m.items)-1)
	}

	next, _ = m.Update(runeKey('k'))
	m = next.(dashboardModel)
	if m.cursor != len(m.items)-2 {
		t.Fatalf("cursor = %d, want %d", m.cursor, len(m.items)-2)
	}
}

func TestDashboardUpdateQuitsOnQOrCtrlC(t *testing.T) {
	for _, key := range []tea.KeyMsg{runeKey('q'), typeKey(tea.KeyCtrlC)} {
		m := newDashboardModel(tango.ProjectStatus{})
		next, cmd := m.Update(key)
		got := next.(dashboardModel)

		if !got.quitting {
			t.Fatalf("quitting = false for key %v, want true", key)
		}
		if got.performed {
			t.Fatalf("performed = true for key %v, want false (quit is not a performed action)", key)
		}
		if cmd == nil {
			t.Fatalf("cmd = nil for key %v, want tea.Quit", key)
		}
	}
}

func TestDashboardUpdateEnterOnAvailableItemWithoutConfirmationPerformsAndQuits(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})
	// cursor starts at menuRunServer, which requires no confirmation.
	next, cmd := m.Update(typeKey(tea.KeyEnter))
	got := next.(dashboardModel)

	if !got.performed {
		t.Fatal("performed = false, want true")
	}
	if !got.quitting {
		t.Fatal("quitting = false, want true")
	}
	if got.action != menuRunServer {
		t.Fatalf("action = %v, want %v", got.action, menuRunServer)
	}
	if cmd == nil {
		t.Fatal("cmd = nil, want tea.Quit")
	}
}

func TestDashboardUpdateEnterOnUnavailableItemDoesNothing(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})
	// Move the cursor to menuShell, the only unavailable item.
	for m.items[m.cursor] != menuShell {
		next, _ := m.Update(typeKey(tea.KeyDown))
		m = next.(dashboardModel)
	}

	next, cmd := m.Update(typeKey(tea.KeyEnter))
	got := next.(dashboardModel)

	if got.performed {
		t.Fatal("performed = true, want false — menuShell is not available")
	}
	if got.quitting {
		t.Fatal("quitting = true, want false")
	}
	if cmd != nil {
		t.Fatal("cmd != nil, want nil")
	}
}

func TestDashboardUpdateEnterOnConfirmationRequiredItemEntersConfirmMode(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})
	for m.items[m.cursor] != menuApplyMigrations {
		next, _ := m.Update(typeKey(tea.KeyDown))
		m = next.(dashboardModel)
	}

	next, cmd := m.Update(typeKey(tea.KeyEnter))
	got := next.(dashboardModel)

	if !got.confirm {
		t.Fatal("confirm = false, want true for a confirmation-required item")
	}
	if got.performed {
		t.Fatal("performed = true, want false before the user answers y/n")
	}
	if cmd != nil {
		t.Fatal("cmd != nil, want nil while awaiting confirmation")
	}
}

func TestDashboardUpdateConfirmYesPerformsAndQuits(t *testing.T) {
	m := dashboardModel{status: tango.ProjectStatus{}, items: menuItems(), confirm: true, action: menuApplyMigrations}

	for _, key := range []tea.KeyMsg{runeKey('y'), runeKey('Y')} {
		next, cmd := m.Update(key)
		got := next.(dashboardModel)

		if !got.confirmed {
			t.Fatalf("confirmed = false for key %v, want true", key)
		}
		if !got.performed {
			t.Fatalf("performed = false for key %v, want true", key)
		}
		if !got.quitting {
			t.Fatalf("quitting = false for key %v, want true", key)
		}
		if cmd == nil {
			t.Fatalf("cmd = nil for key %v, want tea.Quit", key)
		}
	}
}

func TestDashboardUpdateConfirmNoOrEscReturnsToMenu(t *testing.T) {
	for _, key := range []tea.KeyMsg{runeKey('n'), runeKey('N'), typeKey(tea.KeyEsc)} {
		m := dashboardModel{status: tango.ProjectStatus{}, items: menuItems(), confirm: true, action: menuApplyMigrations}

		next, cmd := m.Update(key)
		got := next.(dashboardModel)

		if got.confirm {
			t.Fatalf("confirm = true for key %v, want false (declined)", key)
		}
		if got.performed {
			t.Fatalf("performed = true for key %v, want false", key)
		}
		if cmd != nil {
			t.Fatalf("cmd != nil for key %v, want nil", key)
		}
	}
}

func TestDashboardUpdateConfirmModeIgnoresOtherKeys(t *testing.T) {
	m := dashboardModel{status: tango.ProjectStatus{}, items: menuItems(), confirm: true, action: menuApplyMigrations}

	next, cmd := m.Update(runeKey('x'))
	got := next.(dashboardModel)

	if !got.confirm {
		t.Fatal("confirm = false, want true — an unrecognized key must not leave confirm mode")
	}
	if cmd != nil {
		t.Fatal("cmd != nil, want nil")
	}
}

func TestDashboardUpdateIgnoresUnrecognizedKeyOutsideConfirmMode(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})

	next, cmd := m.Update(runeKey('x'))
	got := next.(dashboardModel)

	if got.cursor != m.cursor || got.quitting || got.performed {
		t.Fatalf("Update(unrecognized key) changed the model, got %+v", got)
	}
	if cmd != nil {
		t.Fatal("cmd != nil, want nil for an unrecognized key")
	}
}

func TestDashboardMoveCursorWraps(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})
	n := len(m.items)

	if got := m.moveCursor(1); got != 1 {
		t.Fatalf("moveCursor(1) from 0 = %d, want 1", got)
	}

	m.cursor = n - 1
	if got := m.moveCursor(1); got != 0 {
		t.Fatalf("moveCursor(1) from last = %d, want 0", got)
	}

	m.cursor = 0
	if got := m.moveCursor(-1); got != n-1 {
		t.Fatalf("moveCursor(-1) from 0 = %d, want %d", got, n-1)
	}
}

func TestDashboardViewRendersStatusAndMenu(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationsTotal:   3,
		MigrationsApplied: 2,
		MigrationsPending: 1,
	})

	out := m.View()

	for _, want := range []string{
		"tanGO project status",
		"registration: ok",
		"database: reachable",
		"3 total, 2 applied, 1 pending",
		"Run server",
		"Apply pending migrations",
		"Roll back the latest migration",
		"Shell (not implemented yet)",
		"move",
		"select",
		"quit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("View() does not contain %q:\n%s", want, out)
		}
	}
}

func TestDashboardViewRendersFailureStates(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{
		RegistrationOK:    false,
		RegistrationError: "boom",
		DatabaseReachable: false,
		DatabaseError:     "no db",
	})

	out := m.View()

	for _, want := range []string{
		"registration: FAILED (boom)",
		"database: UNREACHABLE (no db)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("View() does not contain %q:\n%s", want, out)
		}
	}
}

func TestDashboardInitReturnsNilCommand(t *testing.T) {
	m := newDashboardModel(tango.ProjectStatus{})

	if cmd := m.Init(); cmd != nil {
		t.Fatalf("Init() = %v, want nil", cmd)
	}
}

func TestDashboardViewRendersConfirmationPrompt(t *testing.T) {
	m := dashboardModel{
		status:  tango.ProjectStatus{},
		items:   menuItems(),
		cursor:  1,
		confirm: true,
	}

	out := m.View()

	if !strings.Contains(out, "Apply pending migrations? (y/n)") {
		t.Fatalf("View() does not contain the confirmation prompt:\n%s", out)
	}
}

package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// plainScreen is a dashboard screen as the guide prints it: no escape codes,
// no trailing spaces, no trailing blank lines.
func plainScreen(m dashboardModel) string {
	lines := strings.Split(ansiEscape.ReplaceAllString(m.View(), ""), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// TestTUIGuideShowsWhatTheDashboardPrints keeps docs/guides/tui.md honest:
// every screen below, rendered by the real View with fixed status values, must
// appear verbatim in the guide. The guide is the expected value; the code
// has to reproduce it, so changing the screen without the guide fails here.
func TestTUIGuideShowsWhatTheDashboardPrints(t *testing.T) {
	guide, err := os.ReadFile("../../docs/guides/tui.md")
	if err != nil {
		t.Fatalf("read the guide: %v", err)
	}

	pending := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 3, MigrationsApplied: 2, MigrationsPending: 1}
	upToDate := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 3, MigrationsApplied: 3}
	broken := tango.ProjectStatus{RegistrationError: "apps/blog: duplicate route name \"post\"", DatabaseReachable: true, MigrationsTotal: 3, MigrationsApplied: 3}

	screens := []struct {
		name  string
		model dashboardModel
	}{
		{"a project with a pending migration", newDashboardModel(pending)},
		{"a project that is up to date", newDashboardModel(upToDate)},
		{"a confirmation", dashboardModel{status: pending, items: menuItems(), cursor: 1, confirm: true}},
		{"a failed action", dashboardModel{status: pending, items: menuItems(), notice: "Last action failed: Apply pending migrations (exit code 1): migration 0003_add_title: duplicate column"}},
		{"a project that fails its checks", newDashboardModel(broken)},
	}
	for _, tt := range screens {
		t.Run(tt.name, func(t *testing.T) {
			screen := plainScreen(tt.model)
			if !strings.Contains(string(guide), "```text\n"+screen+"\n```") {
				t.Fatalf("docs/guides/tui.md has no ```text block equal to the screen for %s:\n%s", tt.name, screen)
			}
		})
	}

	// The text printed without a terminal, and the explanation printed when
	// the status cannot be loaded, appear in the guide too.
	var plain strings.Builder
	printStatusPlain(&plain, pending)
	if !strings.Contains(string(guide), "```text\n"+strings.TrimRight(plain.String(), "\n")+"\n```") {
		t.Errorf("docs/guides/tui.md has no ```text block equal to the non-interactive output:\n%s", plain.String())
	}
	if !strings.Contains(string(guide), "```text\n"+strings.TrimRight(statusFailureHelp, "\n")+"\n") {
		t.Errorf("docs/guides/tui.md has no ```text block starting with the status-failure explanation:\n%s", statusFailureHelp)
	}
}

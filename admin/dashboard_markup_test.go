package admin_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func markupBody(t *testing.T) string {
	t.Helper()
	snapshot := activitySnapshot()
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{
		Activity: stubSource{snapshot: snapshot},
		Status: func(context.Context) (admin.DashboardStatus, error) {
			return admin.DashboardStatus{Database: admin.DatabaseReachable, Dialect: "SQLite", TrackingTable: admin.TrackingTablePresent, Release: "v1.2.3"}, nil
		},
	}))
	return getIndex(t, handler).Body.String()
}

// The dashboard is built from the admin's own components, so it reads like
// the list and form pages: a titled .card per section, and label/value rows
// in definition lists.
func TestDashboardUsesTheAdminsCardAndRowStructure(t *testing.T) {
	body := markupBody(t)
	for _, section := range []string{`data-dashboard="status"`, `data-dashboard="activity"`} {
		i := strings.Index(body, section)
		if i < 0 {
			t.Fatalf("missing %s", section)
		}
		open := body[strings.LastIndex(body[:i], "<section"):i]
		if !strings.Contains(open, `class="card"`) {
			t.Errorf("%s is not an admin .card: %q", section, open)
		}
	}
	if got := strings.Count(body, `class="card-header"`); got != 2 {
		t.Errorf("card-header count = %d, want one per section", got)
	}
	if got := strings.Count(body, "<h2 "); got != 2 {
		t.Errorf("h2 count = %d, want one title per section", got)
	}
	for _, tag := range []string{"<dl ", "<dt ", "<dd "} {
		if !strings.Contains(body, tag) {
			t.Errorf("label/value rows lack %s", tag)
		}
	}
	if strings.Count(body, "<dt ") != strings.Count(body, "<dd ") {
		t.Error("every label needs a value")
	}
}

// Every label and value names its colour from the admin's palette, so nothing
// depends on an inherited or browser-default colour, and the page carries no
// decoration the admin does not use.
func TestDashboardTextColoursComeFromTheAdminPaletteWithNoDecoration(t *testing.T) {
	body := markupBody(t)
	start := strings.Index(body, `data-dashboard="page"`)
	if start < 0 {
		t.Fatal("no dashboard page")
	}
	page := body[start : strings.LastIndex(body, "</section>")+len("</section>")]
	for _, tag := range regexp.MustCompile(`<(?:dt|dd|h2|h3|p)\b[^>]*>`).FindAllString(page, -1) {
		if !regexp.MustCompile(`text-(slate|emerald|red|indigo)-\d{3}`).MatchString(tag) {
			t.Errorf("%s names no palette text colour", tag)
		}
	}
	for _, banned := range []string{"gradient", "backdrop-", "shadow-lg", "shadow-xl", "rounded-2xl", "rounded-3xl", "text-3xl", "text-4xl", "text-5xl", "dark:"} {
		if strings.Contains(page, banned) {
			t.Errorf("dashboard markup uses %q, which the admin does not", banned)
		}
	}
}

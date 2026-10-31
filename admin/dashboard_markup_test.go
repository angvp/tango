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

// Every label and value names its colour from the admin's token palette,
// directly or through a named component, so nothing
// depends on an inherited or browser-default colour, and the page carries no
// decoration the admin does not use.
func TestDashboardTextColoursComeFromTheAdminPaletteWithNoDecoration(t *testing.T) {
	body := markupBody(t)
	start := strings.Index(body, `data-dashboard="page"`)
	if start < 0 {
		t.Fatal("no dashboard page")
	}
	page := body[start : strings.LastIndex(body, "</section>")+len("</section>")]
	components := componentsWithText(t)
	for _, tag := range regexp.MustCompile(`<(?:dt|dd|h2|h3|p)\b[^>]*>`).FindAllString(page, -1) {
		m := classAttr.FindStringSubmatch(tag)
		if m == nil || !namesPaletteText(m[1], components) {
			t.Errorf("%s names no palette text colour", tag)
		}
	}
	for _, banned := range []string{"gradient", "backdrop-", "shadow-lg", "shadow-xl", "rounded-2xl", "rounded-3xl", "text-3xl", "text-4xl", "text-5xl", "dark:"} {
		if strings.Contains(page, banned) {
			t.Errorf("dashboard markup uses %q, which the admin does not", banned)
		}
	}
}

// The status dot takes its colour from the status family, so every database
// state reads from a token and none from a raw green, red or grey.
func TestDashboardStatusDotsUseStatusTokens(t *testing.T) {
	cases := []struct {
		name  string
		state admin.DatabaseState
		want  string
	}{
		{"reachable", admin.DatabaseReachable, "before:bg-success-fg"},
		{"unreachable", admin.DatabaseUnreachable, "before:bg-danger-fg"},
		{"unknown", admin.DatabaseUnknown, "before:bg-neutral-fg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{
				Status: func(context.Context) (admin.DashboardStatus, error) {
					return admin.DashboardStatus{Database: tc.state, TrackingTable: admin.TrackingTablePresent}, nil
				},
			}))
			body := getIndex(t, handler).Body.String()
			field := regexp.MustCompile(`<dd [^>]*data-field="database"`).FindString(body)
			if field == "" {
				t.Fatal("no database field on the dashboard")
			}
			if !strings.Contains(field, tc.want) {
				t.Errorf("database dot %q lacks %s", field, tc.want)
			}
		})
	}
}

// No dashboard element names a raw palette colour: every class attribute in
// the dashboard page is free of default-palette utilities.
func TestDashboardCarriesNoRawColourClasses(t *testing.T) {
	body := markupBody(t)
	start := strings.Index(body, `data-dashboard="page"`)
	if start < 0 {
		t.Fatal("no dashboard page")
	}
	page := body[start : strings.LastIndex(body, "</section>")+len("</section>")]
	if raw := rawColourClasses(page); len(raw) > 0 {
		t.Errorf("dashboard uses raw colour classes: %q", raw)
	}
}

// Repeated label/value rows are one named component, not a utility string
// copied into every row.
func TestDashboardRowsUseTheNamedRowComponent(t *testing.T) {
	body := markupBody(t)
	rows := strings.Count(body, "<dt ")
	if got := strings.Count(body, `class="stat-row"`); got != rows {
		t.Errorf("stat-row count = %d, want one per <dt> (%d)", got, rows)
	}
	for _, label := range []string{`<dt class="stat-label"`, `<dd class="stat-value`} {
		if !strings.Contains(body, label) {
			t.Errorf("rows lack %s", label)
		}
	}
	if strings.Contains(body, "sm:grid-cols-[14rem_1fr]") {
		t.Error("rows still carry the copied utility string")
	}
}

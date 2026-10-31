package admin_test

import (
	"context"
	"errors"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/admin"
)

// statusField returns the text inside the dashboard status card element that
// carries data-field=name, and whether that element exists.
func statusField(body, name string) (string, bool) {
	pattern := regexp.MustCompile(`data-field="` + regexp.QuoteMeta(name) + `">(.*?)</dd>`)
	match := pattern.FindStringSubmatch(body)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func statusConfig(status admin.DashboardStatus) admin.DashboardConfig {
	return admin.DashboardConfig{
		Status: func(context.Context) (admin.DashboardStatus, error) { return status, nil },
	}
}

func TestStatusCardRendersEveryField(t *testing.T) {
	started := time.Date(2026, 10, 20, 9, 30, 0, 0, time.FixedZone("UTC+1", 3600))
	handler := buildDashboardHandler(t, admin.WithDashboard(statusConfig(admin.DashboardStatus{
		Database:      admin.DatabaseReachable,
		Dialect:       "postgres",
		Applied:       7,
		Pending:       []string{"0009_add_orders", "0010_index_orders"},
		TrackingTable: admin.TrackingTablePresent,
		Release:       "0.6.0",
		Started:       started,
	})))
	body := getIndex(t, handler).Body.String()

	want := map[string]string{
		"database":      `<span class="badge-success">Reachable</span>`,
		"dialect":       "postgres",
		"applied":       "7",
		"pending":       `<span class="badge-warning">2 pending migrations</span><ul><li>0009_add_orders</li><li>0010_index_orders</li></ul>`,
		"tracking":      "Present",
		"release":       "0.6.0",
		"process-start": "2026-10-20 08:30:00 UTC",
	}
	for field, value := range want {
		got, ok := statusField(body, field)
		if !ok {
			t.Errorf("status card lacks data-field=%q:\n%s", field, body)
			continue
		}
		if got != value {
			t.Errorf("data-field=%q = %q, want %q", field, got, value)
		}
	}
	if !strings.Contains(body, "Process start") {
		t.Error("process start time is not labelled as process start")
	}
	if !strings.Contains(body, `data-state="ready"`) {
		t.Error("ready status card lacks data-state=ready")
	}
}

func TestStatusCardShowsUnreachableDatabaseAsAWordNotHealth(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(statusConfig(admin.DashboardStatus{
		Database:      admin.DatabaseUnreachable,
		TrackingTable: admin.TrackingTableMissing,
	})))
	body := getIndex(t, handler).Body.String()
	if got, _ := statusField(body, "database"); got != `<span class="badge-danger">Unreachable</span>` {
		t.Fatalf("database = %q, want the danger badge Unreachable", got)
	}
	if got, _ := statusField(body, "tracking"); got != `<span class="badge-warning">Missing</span>` {
		t.Fatalf("tracking = %q, want the warning badge Missing", got)
	}
}

func TestPendingMigrationNamesAreEscaped(t *testing.T) {
	const hostile = `<script>alert("x")</script>`
	handler := buildDashboardHandler(t, admin.WithDashboard(statusConfig(admin.DashboardStatus{
		Database:      admin.DatabaseReachable,
		TrackingTable: admin.TrackingTablePresent,
		Pending:       []string{hostile},
	})))
	body := getIndex(t, handler).Body.String()
	if strings.Contains(body, hostile) {
		t.Fatal("a pending migration name reached the page unescaped")
	}
	got, _ := statusField(body, "pending")
	if !strings.Contains(got, html.EscapeString(hostile)) {
		t.Fatalf("pending = %q, want the escaped name", got)
	}
}

func TestEmptyPendingListReadsNoneOnlyWhenTrackingIsPresent(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(statusConfig(admin.DashboardStatus{
		Database:      admin.DatabaseReachable,
		TrackingTable: admin.TrackingTablePresent,
	})))
	body := getIndex(t, handler).Body.String()
	if got, _ := statusField(body, "pending"); got != "None pending" {
		t.Fatalf("pending = %q, want None pending", got)
	}
}

func TestZeroStatusRendersUnknownNeverHealthy(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(statusConfig(admin.DashboardStatus{})))
	body := getIndex(t, handler).Body.String()

	want := map[string]string{
		"database":      `<span class="badge-neutral">Unknown</span>`,
		"dialect":       "Not reported",
		"applied":       `<span class="badge-neutral">Unknown</span>`,
		"pending":       `<span class="badge-neutral">Unknown</span>`,
		"tracking":      `<span class="badge-neutral">Unknown</span>`,
		"release":       "Not reported",
		"process-start": "Not reported",
	}
	for field, value := range want {
		got, ok := statusField(body, field)
		if !ok {
			t.Errorf("zero status lacks data-field=%q", field)
			continue
		}
		if got != value {
			t.Errorf("zero status data-field=%q = %q, want %q", field, got, value)
		}
	}
	for _, healthy := range []string{"None pending", "0001-01-01", "Present", "Reachable"} {
		if strings.Contains(strings.ReplaceAll(body, "Unreachable", ""), healthy) {
			t.Errorf("zero status rendered %q, which reads as healthy", healthy)
		}
	}
}

func TestStatusCardWithoutProviderIsNotConfigured(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{Activity: stubSource{}}))
	body := getIndex(t, handler).Body.String()
	if !strings.Contains(body, `data-dashboard="status" data-state="not-configured"`) {
		t.Fatalf("status card is not marked not-configured:\n%s", body)
	}
	if !strings.Contains(body, "Not configured") {
		t.Fatal("status card does not say not configured")
	}
	if _, ok := statusField(body, "database"); ok {
		t.Fatal("unconfigured status card rendered database field")
	}
}

func TestStatusProviderErrorLeavesNoDSNOnPage(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{
		Status: func(context.Context) (admin.DashboardStatus, error) {
			return admin.DashboardStatus{Database: admin.DatabaseReachable}, errors.New(secretSentinel)
		},
	}))
	body := getIndex(t, handler).Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, secretSentinel) {
		t.Fatal("status provider error text reached the page")
	}
	if !strings.Contains(body, `data-dashboard="status" data-state="unavailable"`) {
		t.Fatal("failed status provider did not render an unavailable card")
	}
	if strings.Contains(body, "Reachable") {
		t.Fatal("failed status provider still reported database state")
	}
}

package admin_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/observability"
)

var activitySince = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func activitySnapshot() observability.Snapshot {
	return observability.Snapshot{
		Scope:  "this instance only, since 2026-10-20 09:00 UTC",
		Since:  activitySince,
		Window: 60 * time.Minute,
		Requests: observability.RequestSummary{
			Total:      42,
			Class2xx:   30,
			Class3xx:   2,
			Class4xx:   7,
			Class5xx:   3,
			LatencyP50: 0.0123,
			LatencyP95: 0.456,
			LatencyP99: 1.2,
		},
		Jobs: observability.JobSummary{Success: 5, Error: 1, Panic: 4, Canceled: 2},
	}
}

func activityBody(t *testing.T, source observability.Snapshotter) string {
	t.Helper()
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{Activity: source}))
	response := getIndex(t, handler)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	return response.Body.String()
}

func TestActivityCardRendersScopeSinceCountsAndSampleCounts(t *testing.T) {
	body := activityBody(t, stubSource{snapshot: activitySnapshot()})

	if !strings.Contains(body, `data-dashboard="activity" data-state="ready"`) {
		t.Fatal("activity card is not in the ready state")
	}
	for _, want := range []string{
		"this instance only, since 2026-10-20 09:00 UTC",
		"2026-10-20 09:00 UTC",
		"60 minutes",
		"2xx", "3xx", "4xx", "5xx",
		"12.3 ms", "456.0 ms", "1200.0 ms",
		"success", "error", "panic", "canceled",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("activity card lacks %q", want)
		}
	}
	if got := strings.Count(body, "42 samples"); got != 3 {
		t.Errorf("sample count beside percentiles = %d, want 3 (one per percentile)", got)
	}
	if strings.Contains(body, "No requests yet") {
		t.Error("a card with requests reads as empty")
	}
}

func TestActivityCardZeroRequestsReadsNoRequestsYet(t *testing.T) {
	snapshot := observability.Snapshot{
		Scope:  "this instance only, since 2026-10-20 09:00 UTC",
		Since:  activitySince,
		Window: 60 * time.Minute,
	}
	body := activityBody(t, stubSource{snapshot: snapshot})

	if !strings.Contains(body, "No requests yet") {
		t.Fatal("zero requests did not read 'No requests yet'")
	}
	if strings.Contains(body, " ms") {
		t.Error("zero requests still shows latency figures")
	}
	if !strings.Contains(body, "No job runs yet") {
		t.Error("zero job runs did not read 'No job runs yet'")
	}
}

func TestActivityCardEscapesScopeText(t *testing.T) {
	snapshot := activitySnapshot()
	snapshot.Scope = "<i>injected</i>"
	body := activityBody(t, stubSource{snapshot: snapshot})

	if strings.Contains(body, "<i>injected</i>") {
		t.Fatal("scope label reached the page unescaped")
	}
	if !strings.Contains(body, "&lt;i&gt;injected&lt;/i&gt;") {
		t.Fatal("escaped scope label missing from the page")
	}
}

func TestActivityCardNotConfiguredWithoutASnapshotter(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{
		Status: func(context.Context) (admin.DashboardStatus, error) {
			return admin.DashboardStatus{Dialect: "sqlite"}, nil
		},
	}))
	body := getIndex(t, handler).Body.String()

	if !strings.Contains(body, `data-dashboard="activity" data-state="not-configured"`) {
		t.Fatal("activity card is not marked not-configured")
	}
	if !strings.Contains(body, "Not configured") {
		t.Fatal("activity card does not say it is not configured")
	}
}

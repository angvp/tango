package admin

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/angvp/tango/observability"
)

// activityCardData backs the activity card: recent request and job activity
// from the host's Snapshotter.
type activityCardData struct {
	State    cardState
	Snapshot observability.Snapshot
}

func loadActivityCard(logger *slog.Logger, source observability.Snapshotter) activityCardData {
	if source == nil {
		return activityCardData{State: cardNotConfigured}
	}
	snapshot, state := runCard(logger, "activity", source.Snapshot)
	return activityCardData{State: state, Snapshot: snapshot}
}

// SinceLabel is the start of the activity window, in UTC.
func (a activityCardData) SinceLabel() string {
	return a.Snapshot.Since.UTC().Format("2006-01-02 15:04 UTC")
}

// WindowLabel is how much recent activity the numbers span.
func (a activityCardData) WindowLabel() string {
	if a.Snapshot.Window < time.Minute {
		return a.Snapshot.Window.String()
	}
	return fmt.Sprintf("%d minutes", int(a.Snapshot.Window/time.Minute))
}

// HasRequests reports whether any request was counted in the window.
func (a activityCardData) HasRequests() bool { return a.Snapshot.Requests.Total > 0 }

// HasJobs reports whether any job invocation was counted in the window.
func (a activityCardData) HasJobs() bool {
	j := a.Snapshot.Jobs
	return j.Success+j.Error+j.Panic+j.Canceled > 0
}

// P50 is the median request latency in milliseconds.
func (a activityCardData) P50() string { return millis(a.Snapshot.Requests.LatencyP50) }

// P95 is the 95th percentile request latency in milliseconds.
func (a activityCardData) P95() string { return millis(a.Snapshot.Requests.LatencyP95) }

// P99 is the 99th percentile request latency in milliseconds.
func (a activityCardData) P99() string { return millis(a.Snapshot.Requests.LatencyP99) }

// millis formats a latency given in seconds as milliseconds.
func millis(seconds float64) string {
	if math.IsInf(seconds, 1) {
		// Beyond the collector's last histogram bucket there is no number.
		return "over 60 s"
	}
	return fmt.Sprintf("%.1f ms", seconds*1000)
}

// The template receives the whole dashboardPageData so it can use .T; its
// own data is .Activity. Every figure is shown as-is: no colour, no threshold.
const activityCardSource = `{{define "activityCard"}}
<section class="card" data-dashboard="activity" data-state="{{.Activity.State}}">
  <div class="card-header">
    <h2 class="section-title">{{.T "admin.dashboard.activity.title" "Activity"}}</h2>
    {{if eq .Activity.State "ready"}}<p class="hint-text"><span data-activity="scope">{{.Activity.Snapshot.Scope}}</span> &middot; <span data-activity="window">{{.T "admin.dashboard.activity.window" "Window since %s, %s" .Activity.SinceLabel .Activity.WindowLabel}}</span></p>{{end}}
  </div>
  {{if eq .Activity.State "unavailable"}}<p class="panel-note-muted">{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Activity.State "not-configured"}}<p class="panel-note">{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{else}}
  <h3 class="section-label">{{.T "admin.dashboard.activity.requests" "Requests"}}</h3>
  {{if .Activity.HasRequests}}
  <dl class="row-list" data-activity="requests">
    <div class="stat-row"><dt class="stat-label">{{.T "admin.dashboard.activity.total" "Total"}}</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Requests.Total}}</dd></div>
    <div class="stat-row"><dt class="stat-label">2xx</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Requests.Class2xx}}</dd></div>
    <div class="stat-row"><dt class="stat-label">3xx</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Requests.Class3xx}}</dd></div>
    <div class="stat-row"><dt class="stat-label">4xx</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Requests.Class4xx}}</dd></div>
    <div class="stat-row"><dt class="stat-label">5xx</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Requests.Class5xx}}</dd></div>
  </dl>
  <h3 class="section-label">{{.T "admin.dashboard.activity.latency" "Latency"}}</h3>
  <dl class="row-list" data-activity="latency">
    <div class="stat-row"><dt class="stat-label">p50</dt><dd class="stat-value stat-value-num">{{.Activity.P50}} <span class="text-ink-faint">({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></dd></div>
    <div class="stat-row"><dt class="stat-label">p95</dt><dd class="stat-value stat-value-num">{{.Activity.P95}} <span class="text-ink-faint">({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></dd></div>
    <div class="stat-row"><dt class="stat-label">p99</dt><dd class="stat-value stat-value-num">{{.Activity.P99}} <span class="text-ink-faint">({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></dd></div>
  </dl>
  {{else}}
  <p class="panel-note-tail">{{.T "admin.dashboard.activity.no_requests" "No requests yet"}}</p>
  {{end}}
  <div class="border-t border-line-subtle"></div>
  <h3 class="section-label">{{.T "admin.dashboard.activity.jobs" "Job runs"}}</h3>
  {{if .Activity.HasJobs}}
  <dl class="row-list pb-2" data-activity="jobs">
    <div class="stat-row"><dt class="stat-label">{{.T "admin.dashboard.activity.job.success" "success"}}</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Jobs.Success}}</dd></div>
    <div class="stat-row"><dt class="stat-label">{{.T "admin.dashboard.activity.job.error" "error"}}</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Jobs.Error}}</dd></div>
    <div class="stat-row"><dt class="stat-label">{{.T "admin.dashboard.activity.job.panic" "panic"}}</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Jobs.Panic}}</dd></div>
    <div class="stat-row"><dt class="stat-label">{{.T "admin.dashboard.activity.job.canceled" "canceled"}}</dt><dd class="stat-value stat-value-num">{{.Activity.Snapshot.Jobs.Canceled}}</dd></div>
  </dl>
  {{else}}
  <p class="panel-note-tail">{{.T "admin.dashboard.activity.no_jobs" "No job runs yet"}}</p>
  {{end}}
  {{end}}
</section>
{{end}}`

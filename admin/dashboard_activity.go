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
    <h2 class="text-sm font-semibold text-slate-900">{{.T "admin.dashboard.activity.title" "Activity"}}</h2>
    {{if eq .Activity.State "ready"}}<p class="text-sm text-slate-500"><span data-activity="scope">{{.Activity.Snapshot.Scope}}</span> &middot; <span data-activity="window">{{.T "admin.dashboard.activity.window" "Window since %s, %s" .Activity.SinceLabel .Activity.WindowLabel}}</span></p>{{end}}
  </div>
  {{if eq .Activity.State "unavailable"}}<p class="px-5 py-4 text-sm text-slate-700">{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Activity.State "not-configured"}}<p class="px-5 py-4 text-sm text-slate-500">{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{else}}
  <h3 class="px-5 pt-4 pb-1 text-xs font-semibold uppercase tracking-wider text-slate-500">{{.T "admin.dashboard.activity.requests" "Requests"}}</h3>
  {{if .Activity.HasRequests}}
  <dl class="divide-y divide-slate-100" data-activity="requests">
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">{{.T "admin.dashboard.activity.total" "Total"}}</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Requests.Total}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">2xx</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Requests.Class2xx}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">3xx</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Requests.Class3xx}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">4xx</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Requests.Class4xx}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">5xx</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Requests.Class5xx}}</dd></div>
  </dl>
  <h3 class="px-5 pt-4 pb-1 text-xs font-semibold uppercase tracking-wider text-slate-500">{{.T "admin.dashboard.activity.latency" "Latency"}}</h3>
  <dl class="divide-y divide-slate-100" data-activity="latency">
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">p50</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.P50}} <span class="text-slate-500">({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">p95</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.P95}} <span class="text-slate-500">({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">p99</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.P99}} <span class="text-slate-500">({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></dd></div>
  </dl>
  {{else}}
  <p class="px-5 pb-4 text-sm text-slate-500">{{.T "admin.dashboard.activity.no_requests" "No requests yet"}}</p>
  {{end}}
  <div class="border-t border-slate-100"></div>
  <h3 class="px-5 pt-4 pb-1 text-xs font-semibold uppercase tracking-wider text-slate-500">{{.T "admin.dashboard.activity.jobs" "Job runs"}}</h3>
  {{if .Activity.HasJobs}}
  <dl class="divide-y divide-slate-100 pb-2" data-activity="jobs">
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">{{.T "admin.dashboard.activity.job.success" "success"}}</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Jobs.Success}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">{{.T "admin.dashboard.activity.job.error" "error"}}</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Jobs.Error}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">{{.T "admin.dashboard.activity.job.panic" "panic"}}</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Jobs.Panic}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-slate-500">{{.T "admin.dashboard.activity.job.canceled" "canceled"}}</dt><dd class="text-right text-sm tabular-nums text-slate-900 sm:text-left">{{.Activity.Snapshot.Jobs.Canceled}}</dd></div>
  </dl>
  {{else}}
  <p class="px-5 pb-4 text-sm text-slate-500">{{.T "admin.dashboard.activity.no_jobs" "No job runs yet"}}</p>
  {{end}}
  {{end}}
</section>
{{end}}`

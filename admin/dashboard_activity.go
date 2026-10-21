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
  <h2>{{.T "admin.dashboard.activity.title" "Activity"}}</h2>
  {{if eq .Activity.State "unavailable"}}<p>{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Activity.State "not-configured"}}<p>{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{else}}
  <p data-activity="scope">{{.Activity.Snapshot.Scope}}</p>
  <p data-activity="window">{{.T "admin.dashboard.activity.window" "Window since %s, %s" .Activity.SinceLabel .Activity.WindowLabel}}</p>
  {{if .Activity.HasRequests}}
  <table data-activity="requests">
    <caption>{{.T "admin.dashboard.activity.requests" "Requests"}}</caption>
    <tbody>
      <tr><th scope="row">{{.T "admin.dashboard.activity.total" "Total"}}</th><td>{{.Activity.Snapshot.Requests.Total}}</td></tr>
      <tr><th scope="row">2xx</th><td>{{.Activity.Snapshot.Requests.Class2xx}}</td></tr>
      <tr><th scope="row">3xx</th><td>{{.Activity.Snapshot.Requests.Class3xx}}</td></tr>
      <tr><th scope="row">4xx</th><td>{{.Activity.Snapshot.Requests.Class4xx}}</td></tr>
      <tr><th scope="row">5xx</th><td>{{.Activity.Snapshot.Requests.Class5xx}}</td></tr>
    </tbody>
  </table>
  <ul data-activity="latency">
    <li>p50 {{.Activity.P50}} <span>({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></li>
    <li>p95 {{.Activity.P95}} <span>({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></li>
    <li>p99 {{.Activity.P99}} <span>({{.T "admin.dashboard.activity.samples" "%d samples" .Activity.Snapshot.Requests.Total}})</span></li>
  </ul>
  {{else}}
  <p>{{.T "admin.dashboard.activity.no_requests" "No requests yet"}}</p>
  {{end}}
  {{if .Activity.HasJobs}}
  <table data-activity="jobs">
    <caption>{{.T "admin.dashboard.activity.jobs" "Job runs"}}</caption>
    <tbody>
      <tr><th scope="row">{{.T "admin.dashboard.activity.job.success" "success"}}</th><td>{{.Activity.Snapshot.Jobs.Success}}</td></tr>
      <tr><th scope="row">{{.T "admin.dashboard.activity.job.error" "error"}}</th><td>{{.Activity.Snapshot.Jobs.Error}}</td></tr>
      <tr><th scope="row">{{.T "admin.dashboard.activity.job.panic" "panic"}}</th><td>{{.Activity.Snapshot.Jobs.Panic}}</td></tr>
      <tr><th scope="row">{{.T "admin.dashboard.activity.job.canceled" "canceled"}}</th><td>{{.Activity.Snapshot.Jobs.Canceled}}</td></tr>
    </tbody>
  </table>
  {{else}}
  <p>{{.T "admin.dashboard.activity.no_jobs" "No job runs yet"}}</p>
  {{end}}
  {{end}}
</section>
{{end}}`

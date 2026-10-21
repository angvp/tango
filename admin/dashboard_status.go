package admin

import (
	"context"
	"log/slog"
)

// statusCardData backs the status cards: database, migrations and release
// from the host's status provider.
type statusCardData struct {
	State  cardState
	Status DashboardStatus
}

func loadStatusCard(logger *slog.Logger, ctx context.Context, provider func(context.Context) (DashboardStatus, error)) statusCardData {
	if provider == nil {
		return statusCardData{State: cardNotConfigured}
	}
	status, state := runCard(logger, "status", func() (DashboardStatus, error) { return provider(ctx) })
	return statusCardData{State: state, Status: status}
}

// The template receives the whole dashboardPageData so it can use .T; its
// own data is .Status.
// Unknown and missing values never read as healthy: an unreported database
// state, tracking table or count says so, and "None pending" appears only when
// the tracking table is present and the pending list is empty.

const statusCardSource = `{{define "statusCard"}}
<section class="card" data-dashboard="status" data-state="{{.Status.State}}">
  <div class="card-header"><h2 class="text-sm font-semibold text-ink">{{.T "admin.dashboard.status.title" "System"}}</h2></div>
  {{if eq .Status.State "unavailable"}}<p class="px-5 py-4 text-sm text-ink-muted">{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Status.State "not-configured"}}<p class="px-5 py-4 text-sm text-ink-faint">{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{else}}<dl class="divide-y divide-line-subtle">
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.database" "Database"}}</dt><dd class="text-right text-sm text-ink sm:text-left font-medium before:mr-2 before:inline-block before:h-2 before:w-2 before:rounded-full {{if eq .Status.Status.Database "reachable"}}before:bg-success-fg{{else if eq .Status.Status.Database "unreachable"}}before:bg-danger-fg{{else}}before:bg-neutral-fg{{end}}" data-field="database">{{if eq .Status.Status.Database "reachable"}}{{.T "admin.dashboard.status.reachable" "Reachable"}}{{else if eq .Status.Status.Database "unreachable"}}{{.T "admin.dashboard.status.unreachable" "Unreachable"}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.dialect" "Dialect"}}</dt><dd class="text-right text-sm text-ink sm:text-left" data-field="dialect">{{if .Status.Status.Dialect}}{{.Status.Status.Dialect}}{{else}}{{.T "admin.dashboard.status.not_reported" "Not reported"}}{{end}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.applied" "Applied migrations"}}</dt><dd class="text-right text-sm text-ink sm:text-left tabular-nums" data-field="applied">{{if eq .Status.Status.TrackingTable "present"}}{{.Status.Status.Applied}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.pending" "Pending migrations"}}</dt><dd class="text-right text-sm text-ink sm:text-left [&_ul]:space-y-1 [&_ul]:font-mono [&_ul]:text-xs" data-field="pending">{{if .Status.Status.Pending}}<ul>{{range .Status.Status.Pending}}<li>{{.}}</li>{{end}}</ul>{{else if eq .Status.Status.TrackingTable "present"}}{{.T "admin.dashboard.status.none_pending" "None pending"}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.tracking_table" "Migration tracking table"}}</dt><dd class="text-right text-sm text-ink sm:text-left" data-field="tracking">{{if eq .Status.Status.TrackingTable "present"}}{{.T "admin.dashboard.status.present" "Present"}}{{else if eq .Status.Status.TrackingTable "missing"}}{{.T "admin.dashboard.status.missing" "Missing"}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.release" "tanGO release"}}</dt><dd class="text-right text-sm text-ink sm:text-left" data-field="release">{{if .Status.Status.Release}}{{.Status.Status.Release}}{{else}}{{.T "admin.dashboard.status.not_reported" "Not reported"}}{{end}}</dd></div>
    <div class="flex items-baseline justify-between gap-4 px-5 py-2.5 sm:grid sm:grid-cols-[14rem_1fr] sm:justify-start"><dt class="text-sm text-ink-faint">{{.T "admin.dashboard.status.process_start" "Process start (UTC)"}}</dt><dd class="text-right text-sm text-ink sm:text-left tabular-nums" data-field="process-start">{{if .Status.Status.Started.IsZero}}{{.T "admin.dashboard.status.not_reported" "Not reported"}}{{else}}{{.Status.Status.Started.UTC.Format "2006-01-02 15:04:05 UTC"}}{{end}}</dd></div>
  </dl>
  {{end}}
</section>
{{end}}`

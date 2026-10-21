package admin

import "context"

// statusCardData backs the status cards: database, migrations and release
// from the host's status provider.
type statusCardData struct {
	State  cardState
	Status DashboardStatus
}

func loadStatusCard(ctx context.Context, provider func(context.Context) (DashboardStatus, error)) statusCardData {
	if provider == nil {
		return statusCardData{State: cardNotConfigured}
	}
	status, state := runCard("status", func() (DashboardStatus, error) { return provider(ctx) })
	return statusCardData{State: state, Status: status}
}

// The template receives the whole dashboardPageData so it can use .T; its
// own data is .Status.
// Unknown and missing values never read as healthy: an unreported database
// state, tracking table or count says so, and "None pending" appears only when
// the tracking table is present and the pending list is empty.
const statusCardSource = `{{define "statusCard"}}
<section class="card" data-dashboard="status" data-state="{{.Status.State}}">
  {{if eq .Status.State "unavailable"}}<p>{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Status.State "not-configured"}}<p>{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{else}}<dl>
    <dt>{{.T "admin.dashboard.status.database" "Database"}}</dt>
    <dd data-field="database">{{if eq .Status.Status.Database "reachable"}}{{.T "admin.dashboard.status.reachable" "Reachable"}}{{else if eq .Status.Status.Database "unreachable"}}{{.T "admin.dashboard.status.unreachable" "Unreachable"}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd>
    <dt>{{.T "admin.dashboard.status.dialect" "Dialect"}}</dt>
    <dd data-field="dialect">{{if .Status.Status.Dialect}}{{.Status.Status.Dialect}}{{else}}{{.T "admin.dashboard.status.not_reported" "Not reported"}}{{end}}</dd>
    <dt>{{.T "admin.dashboard.status.applied" "Applied migrations"}}</dt>
    <dd data-field="applied">{{if eq .Status.Status.TrackingTable "present"}}{{.Status.Status.Applied}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd>
    <dt>{{.T "admin.dashboard.status.pending" "Pending migrations"}}</dt>
    <dd data-field="pending">{{if .Status.Status.Pending}}<ul>{{range .Status.Status.Pending}}<li>{{.}}</li>{{end}}</ul>{{else if eq .Status.Status.TrackingTable "present"}}{{.T "admin.dashboard.status.none_pending" "None pending"}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd>
    <dt>{{.T "admin.dashboard.status.tracking_table" "Migration tracking table"}}</dt>
    <dd data-field="tracking">{{if eq .Status.Status.TrackingTable "present"}}{{.T "admin.dashboard.status.present" "Present"}}{{else if eq .Status.Status.TrackingTable "missing"}}{{.T "admin.dashboard.status.missing" "Missing"}}{{else}}{{.T "admin.dashboard.status.unknown" "Unknown"}}{{end}}</dd>
    <dt>{{.T "admin.dashboard.status.release" "tanGO release"}}</dt>
    <dd data-field="release">{{if .Status.Status.Release}}{{.Status.Status.Release}}{{else}}{{.T "admin.dashboard.status.not_reported" "Not reported"}}{{end}}</dd>
    <dt>{{.T "admin.dashboard.status.process_start" "Process start (UTC)"}}</dt>
    <dd data-field="process-start">{{if .Status.Status.Started.IsZero}}{{.T "admin.dashboard.status.not_reported" "Not reported"}}{{else}}{{.Status.Status.Started.UTC.Format "2006-01-02 15:04:05 UTC"}}{{end}}</dd>
  </dl>
  {{end}}
</section>
{{end}}`

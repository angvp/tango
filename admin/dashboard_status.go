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
const statusCardSource = `{{define "statusCard"}}
<section class="card" data-dashboard="status" data-state="{{.Status.State}}">
  {{if eq .Status.State "unavailable"}}<p>{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Status.State "not-configured"}}<p>{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{end}}
</section>
{{end}}`

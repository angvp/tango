package admin

import "github.com/angvp/tango/observability"

// activityCardData backs the activity card: recent request and job activity
// from the host's Snapshotter.
type activityCardData struct {
	State    cardState
	Snapshot observability.Snapshot
}

func loadActivityCard(source observability.Snapshotter) activityCardData {
	if source == nil {
		return activityCardData{State: cardNotConfigured}
	}
	snapshot, state := runCard("activity", source.Snapshot)
	return activityCardData{State: state, Snapshot: snapshot}
}

// The template receives the whole dashboardPageData so it can use .T; its
// own data is .Activity.
const activityCardSource = `{{define "activityCard"}}
<section class="card" data-dashboard="activity" data-state="{{.Activity.State}}">
  {{if eq .Activity.State "unavailable"}}<p>{{.T "admin.dashboard.unavailable" "Unavailable"}}</p>
  {{else if eq .Activity.State "not-configured"}}<p>{{.T "admin.dashboard.not_configured" "Not configured"}}</p>
  {{end}}
</section>
{{end}}`

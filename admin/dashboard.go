package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/observability"
)

// DatabaseState is whether the host's status provider could reach its
// database.
type DatabaseState string

// The values a status provider reports for the database.
const (
	DatabaseReachable   DatabaseState = "reachable"
	DatabaseUnreachable DatabaseState = "unreachable"
	DatabaseUnknown     DatabaseState = "unknown"
)

// TrackingTableState is whether the migration tracking table exists.
type TrackingTableState string

// The values a status provider reports for the tracking table.
const (
	TrackingTablePresent TrackingTableState = "present"
	TrackingTableMissing TrackingTableState = "missing"
	TrackingTableUnknown TrackingTableState = "unknown"
)

// DashboardStatus is the safe display data a status provider hands the
// dashboard. It holds plain values only: no DSN, credential, error text or
// host metadata, so nothing in it can leak what the host did not choose to
// show.
type DashboardStatus struct {
	Database      DatabaseState
	Dialect       string
	Applied       int
	Pending       []string
	TrackingTable TrackingTableState
	// Release is the tanGO version the application was built with.
	Release string
	// Started is when this process started. It is not the start of the
	// Activity window, which Snapshot.Since reports.
	Started time.Time
}

// DashboardConfig selects what the optional dashboard shows. Either field may
// be nil; the dashboard exists only when at least one is set.
type DashboardConfig struct {
	// Status reports database and migration state. The host builds it from
	// the values it owns, for example with tango.Status. Without it the
	// status cards say they are not configured.
	Status func(context.Context) (DashboardStatus, error)
	// Activity reports recent request and job activity, for example an
	// observability.Window. Without it the activity card says so.
	Activity observability.Snapshotter
}

func (c DashboardConfig) enabled() bool { return c.Status != nil || c.Activity != nil }

// WithDashboard turns the admin landing page into an operational dashboard.
// An app that omits it, or passes a config with neither source, keeps the
// landing page it had before.
func WithDashboard(cfg DashboardConfig) Option {
	return func(c *adminConfig) { c.dashboard = cfg }
}

// cardState is how one dashboard card renders.
type cardState string

const (
	cardReady         cardState = "ready"
	cardUnavailable   cardState = "unavailable"
	cardNotConfigured cardState = "not-configured"
)

// runCard calls a host-provided callback for one card. An error or a panic is
// logged on the server (through the request's logger) and becomes the card's generic unavailable state; the
// text of either never reaches the page.
func runCard[T any](logger *slog.Logger, card string, fn func() (T, error)) (value T, state cardState) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			logger.Error("admin dashboard: card panicked", "card", card, "panic", fmt.Sprint(r))
			value, state = zero, cardUnavailable
		}
	}()
	value, err := fn()
	if err != nil {
		var zero T
		logger.Error("admin dashboard: card failed", "card", card, "err", err)
		return zero, cardUnavailable
	}
	return value, cardReady
}

type dashboardPageData struct {
	chrome
	Activity activityCardData
	Status   statusCardData
}

func dashboardView(nav []navItem, brand Branding, cfg DashboardConfig) tango.View {
	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodGet {
			return methodNotAllowed(ctx)
		}
		return render(ctx, http.StatusOK, dashboardTemplate, dashboardPageData{
			chrome:   chrome{Nav: nav, Brand: brand}.withContext(ctx.Context()),
			Activity: loadActivityCard(ctx.Logger(), cfg.Activity),
			Status:   loadStatusCard(ctx.Logger(), ctx.Context(), cfg.Status),
		})
	}
}

var dashboardTemplate = cloneWithContent(dashboardSource + cardNoticeSource + activityCardSource + statusCardSource)

// The two card notices are shared by the status and activity cards. An
// unavailable card is a failure, so it is announced as an alert; a card with
// no source is informational, so it is a status.
const cardNoticeSource = `{{define "cardUnavailable"}}<div class="card-body"><p class="alert-warning" role="alert">{{.T "admin.dashboard.unavailable" "Unavailable"}}</p></div>{{end}}
{{define "cardNotConfigured"}}<div class="card-body"><p class="alert-neutral">{{.T "admin.dashboard.not_configured" "Not configured"}}</p></div>{{end}}`

const dashboardSource = `{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.T "admin.dashboard.title" "Dashboard"}}</h1>
  </div>
</header>
<div class="content-body space-y-6" data-dashboard="page">
  {{template "statusCard" .}}
  {{template "activityCard" .}}
</div>
{{end}}`

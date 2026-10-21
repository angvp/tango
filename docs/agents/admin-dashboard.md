# Agent Recipe: admin dashboard

Use this when a project wants an operational overview at `/admin/`, or when changing the dashboard, `observability.Window`, `Snapshotter` or `Multi`.

Canonical files: `admin/dashboard.go` (config, state helper), `admin/dashboard_status.go`, `admin/dashboard_activity.go`, `observability/snapshot.go`, `observability/window.go`, `observability/multi.go`. Human guide: `docs/guides/admin-dashboard.md`. Example: `examples/api-with-admin`.

## Enable it

- Build one `observability.NewWindow()`. Pass `observability.Multi(window, ...)` to `tango.WithRecorder`, and `window` as `admin.DashboardConfig.Activity`.
- Supply `DashboardConfig.Status` from `main.go`: it needs the `Config`, `*sql.DB`, dialect and migration list, which `admin` does not have. Build it from `tango.Status` (see `examples/api-with-admin/dashboard.go`).
- Either field may be nil. With neither set, `/admin/` still redirects to the first model.

## Rules

- `DashboardStatus` carries plain values only. Never add a field for a DSN, an error string or host metadata.
- A callback error or panic makes that one card "unavailable" and is logged server-side. Never render error text.
- `Window` keeps global aggregates only. Do not add route, method, status code, job name, user or room to it.
- Every activity number carries the source's `Scope` label. A `Window` is "this instance only"; it resets on restart and is not a history. Point people at an external monitoring system for history, alerts and several instances.
- `Snapshot.Since` (activity window start) and `DashboardStatus.Started` (process start) are different things; do not conflate them.
- `Multi` does not recover panics.

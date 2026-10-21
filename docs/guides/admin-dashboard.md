# Guide: the admin dashboard

The admin's landing page can be an operational overview: is the database reachable, are migrations pending, and is this instance seeing errors or slow requests right now. It is opt-in, small on purpose, and answers "is this application basically healthy?". It is not a metrics store, an alerting tool or a replacement for external monitoring.

## Enable it

```go
window := observability.NewWindow()

dashboard := admin.DashboardConfig{
	Status:   dashboardStatus(...), // func(context.Context) (admin.DashboardStatus, error)
	Activity: window,               // any observability.Snapshotter
}
config.InstalledApps = append(config.InstalledApps, admin.New(store, admin.WithDashboard(dashboard)))

// Feed the Window every framework metric; Multi also feeds an exporter.
tango.ServeContext(ctx, config, sqlDB, dialect, tango.WithRecorder(observability.Multi(window, yourExporter)))
```

Either field may be nil. With neither set, or without `WithDashboard`, `/admin/` behaves as before and redirects to the first registered model. The dashboard is staff-only, like every other admin page.

[`examples/api-with-admin`](../../examples/api-with-admin) wires all of this.

## The status cards

`admin` does not own your `Config`, your `*sql.DB`, your dialect or your migration list, so it cannot work out database or migration state itself. You supply a function that does, usually from the values `main.go` already has and `tango.Status`:

```go
func(ctx context.Context) (admin.DashboardStatus, error)
```

`DashboardStatus` holds plain display data only: the database state (`DatabaseReachable`, `DatabaseUnreachable`, `DatabaseUnknown`), the dialect, the applied count, the pending migration names, the tracking-table state, the tanGO release and the process start time. It has no field for a DSN or an error message, so neither can reach the page. Without a function, the status cards say "not configured"; they never claim the application is healthy.

`Started` is when the process started. It is not the start of the activity window below.

## The activity card

`observability.Window` is an opt-in, in-memory collector. Pass it where a `Recorder` goes (`tango.WithRecorder`) and as `DashboardConfig.Activity`. It keeps the last 60 minutes in one-minute buckets:

- HTTP requests, counted by status class (2xx to 5xx), with p50, p95 and p99 latency;
- scheduler job runs, counted by outcome (`success`, `error`, `panic`, `canceled`).

Nothing else is kept: no route, method, status code, job name, user or room. The page shows the sample count beside every percentile, "no requests yet" when there are none, and a latency past the last histogram bucket as "over 60 s". It does not colour anything as good or bad, because tanGO has no thresholds to compare against.

### What the numbers cover

Every activity number carries a scope label, and for a `Window` it reads "this instance only". The numbers cover one process, start again from zero when it restarts, and are not a history. Behind a load balancer with several instances, each instance's dashboard shows only itself.

`observability.Snapshotter` is the read side, separate from `Recorder`: `Snapshot() (Snapshot, error)`. A shared source (a store several instances write to) can implement it and declare a wider `Scope` in the snapshot; the dashboard shows exactly the label the source gives and adds no claim of its own. `Snapshot.Since` is when the source's window began.

## When a card fails

A function that returns an error, or panics, marks only its own card "unavailable". The page still renders, the cause is logged on the server, and neither the error text nor a panic value is shown in the page.

## When to use something else

Use an external monitoring system when you need history, alerts, several instances in one view, or traces and logs. The `Window` is a way to answer a quick question without deploying one. See the [observability guide](observability.md) for the metrics tanGO emits to an exporter, and [`Multi`](../reference.md#observability-githubcomangvptangoobservability) to feed both.

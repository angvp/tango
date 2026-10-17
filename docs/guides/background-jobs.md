# Background jobs

A `tango.Job` is a name, an interval and a function. An app records it from its `Register` callback, and `tango.ServeContext` runs it on a ticker for as long as the server is up. Use it for small, in-process recurring work: pruning expired rows, a periodic reconciliation, refreshing something the app owns.

The runnable proof is the `housekeeping` app of [`examples/board`](../../examples/board), built step by step in [tutorial part 9](../tutorial/09-jobs-logging-and-shutdown.md).

## Registering a job

```go
func (App) Register(registry *tango.Registry) error {
	return registry.RegisterJob(tango.Job{
		Name:     "prune-expired-sessions",
		Interval: time.Hour,
		Run: func(ctx context.Context) error {
			return prune(ctx)
		},
	})
}
```

`RegisterJob` only records the job and never calls `Run`. It returns an error when `Name` is blank, `Interval` is not positive, `Run` is nil, or another job already has the same `Name`. Names appear in logs and metrics, so keep them stable.

## When it runs

- **`ServeContext` only.** The scheduler starts and stops with `ServeContext`. A host that calls `tango.Serve` never runs its jobs, because `Serve` has no shutdown path to stop them with. See [application lifecycle](application-lifecycle.md).
- **The first run is one interval after startup**, not at startup. If a job must also run once at boot, call the same function yourself from a `Lifecycle` start hook.
- **A slow run never piles up.** If a run is still going when the next tick arrives, that tick is dropped: not queued, not replayed. The next run starts on the first tick after the previous one finished.
- **Each job has its own ticker.** One job being slow does not delay another.

## Errors, panics and shutdown

- A returned error, or a panic, is logged at error level as the event `tango.scheduler.job_failed` (`tango.EventJobFailed`) with the attributes `job`, `error` and `panicked`. The next tick runs on schedule anyway.
- The `ctx` passed to `Run` is cancelled when the application shuts down. An error that matches that cancellation is treated as an expected stop, not a failure: nothing is logged for it. Pass `ctx` to your database calls and other blocking work so a run in progress stops promptly.
- When shutdown begins, no new run starts, and the scheduler waits for a run already in progress to return. A `Run` that ignores `ctx.Done()` therefore holds shutdown up.

## Metrics

With a recorder installed (`tango.WithRecorder`), every run adds 1 to the counter `tango_scheduler_job_invocations_total` (`observability.MetricSchedulerJobInvocations`) with the attributes `job` and `outcome`, where `outcome` is one of `success`, `error`, `panic` or `canceled`. See [observability](observability.md).

## What it is not

Jobs are in-process only. There is no cron syntax, retry engine, persistence of what ran, distributed coordination or status page. If you run more than one server process, every process runs every job, so make the work safe to repeat; see [running more than one instance](running-more-than-one-instance.md). Do not depend on a job for exactly-once delivery or for time-critical work.

## Testing a job

Test the function itself, outside the scheduler: call `prune(ctx)` against a test database (see the testing helpers in [`testdb`](../reference.md#testdb-githubcomangvptangotestdb)), once with a context that is cancelled to check it stops, and once with a failing dependency to check the error it returns.

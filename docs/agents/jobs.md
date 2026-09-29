# Agent Recipe: Background Jobs

Use this for small, in-process recurring maintenance: pruning expired rows, periodic reconciliation, or an app-owned refresh task.

Canonical example: `docs/tutorial/09-jobs-logging-and-shutdown.md`. Human lifecycle guide: `docs/guides/application-lifecycle.md`.

## Build

- Register `tango.Job{Name, Interval, Run}` from an app's `Register` callback.
- Give every Job a stable, non-empty unique name and a positive interval.
- Make `Run(ctx)` bounded and cancellation-aware; return meaningful errors.
- Start the host with `tango.ServeContext`, not `tango.Serve`; the scheduler only runs as part of the `ServeContext` lifecycle.

```go
return registry.RegisterJob(tango.Job{
    Name:     "prune-expired-sessions",
    Interval: time.Hour,
    Run: func(ctx context.Context) error {
        return prune(ctx)
    },
})
```

## Runtime behavior

- The first run is one interval after startup.
- A run that overlaps its next tick is not queued; that tick is skipped.
- A panic or ordinary error is reported and does not stop later ticks.
- A returned error matching the application context's cancellation is expected shutdown, not a job failure.
- Jobs are in-process only: no cron syntax, retry engine, persistence, distributed coordination, or status dashboard.

## Don't

- Do not start your own permanent scheduler goroutine from `Register`.
- Do not depend on a Job for exactly-once delivery or time-critical work.
- Do not ignore `ctx.Done()` in a long-running operation; shutdown waits for cooperative work to finish.

## Check

- Test the Job function independently of the scheduler.
- Exercise an error path and a cancellation-aware path.
- Use `docs/agents/application-lifecycle.md` when the host does not already call `ServeContext`.
- Run `docs/agents/checklist.md`.

# Agent Recipe: Application Lifecycle and Graceful Shutdown

Use this when a host needs a background component (a hub, a scheduler, a worker pool) started/stopped in step with the HTTP server, or wants graceful shutdown on `SIGTERM`/Ctrl-C.

Canonical example: `examples/realtime-chat`. Human guide: `docs/guides/application-lifecycle.md`.

## Build

- Register a `tango.Lifecycle{Name, Start, Stop}` via `registry.RegisterLifecycle(...)` from inside an `App.Register` callback — never call `Start`/`Stop` yourself, and never register outside `Register`.
- `Start`/`Stop` are each independently optional — nil is skipped — but at least one must be set. `Name` must be non-empty and unique.
- Swap `tango.Serve(config, sqlDB, dialect)` for `tango.ServeContext(ctx, config, sqlDB, dialect, opts...)` where `ctx` is caller-cancelable.
- Build `ctx` with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` in `main` — tanGO installs no signal handling itself.
- Use `tango.WithShutdownTimeout(d)` to override the default 15s per-phase budget; `d` must be positive.

## Choose The Shape

- One `Lifecycle` per independently-stoppable component — a chat app with both a `realtime.Hub` and a scheduler registers two, not one combined `Stop`.
- Wrapping an already-constructed component (e.g. `hub.Close`) needs only `Stop`; a component you construct as part of starting needs `Start` too.
- Keep `Serve` (not `ServeContext`) for a host with no `Lifecycle` components and no need for graceful shutdown — it's still the simplest path and stays fully compatible.

## Don't

- Do not assume the context passed to `Start` is a child of the caller's `ctx` — it's the decoupled Application context (`context.WithoutCancel(ctx)` plus `ServeContext`'s own cancellation), canceled only after HTTP draining finishes, not the instant the caller cancels.
- Do not assume one `WithShutdownTimeout` covers the whole shutdown — HTTP draining and `Lifecycle.Stop` calls each get their own independent budget; worst case is ~2x the configured value.
- Do not rely on framework-installed signal handling, a restart policy, or `-tango-status` reporting lifecycle state — none of that exists; see `docs/limitations.md`.
- Do not register a `Lifecycle` with a blank `Name` or both callbacks nil — `RegisterLifecycle` rejects both outright rather than normalizing them.

## Check

- Exercise: `Start` failure rolls back already-started components in reverse order; caller cancellation mid-startup does the same.
- Exercise: normal shutdown drains, then cancels the Application context, then stops components in reverse order — assert ordering via a log across at least two components.
- Verify `errors.Is` finds `ErrDuplicateLifecycle` on a repeated name, and finds the original `Start`/serve error plus each wrapped `Stop` error through a joined result.
- Run `docs/agents/checklist.md` before stopping.

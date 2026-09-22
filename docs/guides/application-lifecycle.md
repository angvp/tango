# Application lifecycle and graceful shutdown

`ServeContext` runs your app the same way `Serve` does, but additionally starts and stops any background components you register as `Lifecycle` values, and shuts down gracefully when its context is canceled — draining in-flight HTTP requests before tearing components down, instead of the process exiting mid-request.

`Serve` itself is unchanged: it delegates to `ServeContext(context.Background(), ...)`, so it still blocks forever with no caller-triggered shutdown path. Use `ServeContext` directly when you want graceful shutdown — typically driven by an OS signal.

The runnable reference is [`examples/realtime-chat`](../../examples/realtime-chat), which registers its `realtime.Hub`'s `Close` as a `Lifecycle` and wires `signal.NotifyContext` into `main`.

## Registering a `Lifecycle`

```go
type Lifecycle struct {
    Name  string
    Start func(context.Context) error
    Stop  func(context.Context) error
}
```

Register one from inside an `App.Register` callback, the same place you register routes and models:

```go
app := tango.NewApp("chat", func(registry *tango.Registry) error {
    if err := registry.Routes().Include(/* ... */); err != nil {
        return err
    }
    return registry.RegisterLifecycle(tango.Lifecycle{
        Name: "chat-hub",
        Stop: hub.Close,
    })
})
```

`RegisterLifecycle` never starts anything — it only records the component. `Start` and `Stop` are each independently optional (a `Stop`-only `Lifecycle`, as above, is exactly what wrapping an already-constructed component needs), but at least one of them must be set, and `Name` must be non-empty and unique across every registered `Lifecycle` — a repeated name returns an error wrapping `ErrDuplicateLifecycle`, checkable via `errors.Is`. See [ADR 0030](../adr/0030-lifecycle-is-explicit-registry-registration-not-an-optional-app-interface.md) for why this is explicit registration rather than an optional interface on `App` (the same shape `Checker` uses).

## Startup and shutdown order

`ServeContext` runs `Lifecycle.Start` (skipping any nil) in registration order — forward across every `App.Register` callback, matching `InstalledApps` order. If a `Start` fails, or the caller's context is canceled before every `Start` has run, every component that already started (a nil `Start` counts as trivially started) is stopped in reverse order, and the HTTP server is never started at all.

Once the server is running, canceling `ctx` triggers shutdown:

1. Drain in-flight HTTP requests via `http.Server.Shutdown`, bounded by `WithShutdownTimeout` (default 15s). If the drain deadline is hit, the server is force-closed instead.
2. Only now is the **Application context** (below) canceled.
3. Every started `Lifecycle.Stop` runs in reverse registration order.
4. Every real error along the way (the original `Start`/serve error, the drain/close error, each `Stop` error) is joined via `errors.Join` and returned. A clean caller-triggered shutdown with no real failures returns `nil`.

## Application context vs. shutdown context

The context passed to every `Lifecycle.Start` — and that a component may retain for its own background goroutines — is deliberately **not** a direct child of the `ctx` you pass to `ServeContext`. It's built from `context.WithoutCancel(ctx)` plus `ServeContext`'s own cancellation, so a component's in-flight work isn't torn down the instant you cancel `ctx`; it only ends once HTTP draining (step 1 above) has actually finished. If it were a direct child, canceling `ctx` — the normal shutdown trigger — would cancel every component's context before draining even begins, which could pull the rug out from under a request still being handled.

Each shutdown phase instead gets its own **fresh, independent** context bounded by `WithShutdownTimeout`: one for the HTTP drain, a separate one later for the `Lifecycle.Stop` calls. This is not one timeout split across both phases — draining a slow connection down to the wire doesn't leave your components with an already-expired context to clean up in. The tradeoff: a graceful shutdown can take up to roughly **2x** the configured timeout in the worst case. See [ADR 0031](../adr/0031-shutdown-uses-two-independent-phase-timeouts-and-a-decoupled-application-context.md).

## Wiring signals yourself

tanGO never installs OS signal handling for you — a host wires it with the standard library, then passes the resulting context to `ServeContext`:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := tango.ServeContext(ctx, config, sqlDB, dialect); err != nil {
    log.Fatal(err)
}
```

`examples/realtime-chat/main.go` does exactly this — Ctrl-C or `SIGTERM` now drains in-flight WebSocket connections and calls `hub.Close` before the process exits, instead of the connection just vanishing.

## What this isn't

No process supervisor or restart policy — that's your process manager's or container orchestrator's job, not tanGO's. No lifecycle status surfaced in `-tango-status`. See [limitations](../limitations.md) for the full list.

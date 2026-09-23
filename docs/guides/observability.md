# Structured logging and observability

tanGO uses the standard library's `log/slog` for structured logs and a small backend-neutral `observability.Recorder` interface for metrics. It does not require Prometheus, OpenTelemetry, or a particular logging backend.

## Server configuration

Pass the framework logger and metric recorder to `ServeContext`:

```go
err := tango.ServeContext(ctx, config, sqlDB, dialect,
    tango.WithLogger(logger),
    tango.WithRecorder(recorder),
)
```

An omitted logger uses `slog.Default()`. `WithLogger(nil)` is a configuration error. Metrics are disabled unless a non-no-op recorder is configured. Recorder implementations must be safe for concurrent use.

`WithRecorder` covers matched HTTP routes and scheduler jobs. Realtime is an independent package and receives its recorder through `realtime.Options{Recorder: recorder}`. Both host/bot `Dispatch` and transport-facing `DispatchPeer` calls contribute to the bounded `dispatch` outcome metric.

## Request middleware

The recommended order is:

```go
Middleware: []tango.Middleware{
    tango.RequestID(),
    tango.Recoverer(),
    tango.AccessLogger(),
}
```

`RequestID` generates a cryptographically random ID, exposes it through `RequestIDFromContext`, and writes `X-Request-ID`. It never trusts an inbound header by default. If a trusted proxy supplies IDs, opt in explicitly:

```go
tango.RequestID(tango.WithTrustedHeader("X-Gateway-Request-ID"))
```

The header name is validated when middleware is constructed. Invalid inbound values are ignored and replaced with a generated ID.

`Recoverer` converts a panic into tanGO's generic JSON 500 response and emits `tango.EventPanic`. `AccessLogger` emits one `tango.EventAccessLog` after the downstream matched route completes. Each middleware can receive its own logger through `WithRequestIDLogger`, `WithRecoveryLogger`, or `WithAccessLogger`; otherwise it uses `slog.Default()`.

Inside a View, `ctx.Logger()` returns the configured logger enriched with the route pattern, HTTP method, and request ID when present.

## Stable events and metrics

The stable log event names are `EventViewError`, `EventPanic`, `EventAccessLog`, `EventJobFailed`, and `EventRequestIDGenerationFailed`. The stable metric names are `MetricHTTPRequestDuration`, `MetricSchedulerJobInvocations`, and `MetricRealtimeRoomEvents` in the `observability` package.

Framework metric labels are intentionally bounded. HTTP metrics use route patterns rather than raw URLs. Realtime metrics never include room IDs, user IDs, or error text. Durations are recorded in seconds.

Host-supplied log handlers and recorders are best-effort dependencies: a panic from one is isolated and cannot replace the application error or operation being reported.

## Known boundary

HTTP instrumentation wraps compiled, matched routes. Router-generated 404 and 405 responses do not pass through `AccessLogger` and do not produce automatic HTTP metrics in this version.

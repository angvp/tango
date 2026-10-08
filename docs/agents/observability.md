# Observability recipe

1. Use `log/slog`; do not introduce a framework-specific logger abstraction.
2. Pass the application logger with `tango.WithLogger(logger)` when calling `ServeContext`.
3. Implement `observability.Recorder` to bridge metrics to the host's backend, then pass it with `tango.WithRecorder(recorder)`.
4. Install `RequestID`, `Recoverer`, and `AccessLogger` in that order when all three are needed.
5. Use `ctx.Logger()` inside Views instead of rebuilding request attributes.
6. Configure realtime metrics separately with `realtime.Options{Recorder: recorder}`.
7. Keep metric attributes bounded: route patterns and fixed outcome vocabularies only; never raw URLs, IDs, tokens, or error messages.
8. Router-generated 404/405 responses (Unmatched requests) are observed only under `Config.MiddlewareScope = tango.MiddlewareScopeAll`, which `tango newproject` sets. They report the fixed route value `"(unmatched)"`; never log the raw path as the route instead. Under the default (`MiddlewareScopeRoutes`), they skip global middleware, access logs and metrics.
9. The `route` attribute is the registered pattern, trailing slash included (`/items/{id}/`), and is the same in `ctx.Logger()`, View-error, access, panic and request-ID logs and in metrics.
10. A body rejected by `tango.MaxBodySize` gets `413` and is not logged as a View error: it is the client's error, not an application failure.

Canonical explanation and examples: `docs/guides/observability.md`. Public symbols: `docs/reference.md`.

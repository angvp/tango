# Observability recipe

1. Use `log/slog`; do not introduce a framework-specific logger abstraction.
2. Pass the application logger with `tango.WithLogger(logger)` when calling `ServeContext`.
3. Implement `observability.Recorder` to bridge metrics to the host's backend, then pass it with `tango.WithRecorder(recorder)`.
4. Install `RequestID`, `Recoverer`, and `AccessLogger` in that order when all three are needed.
5. Use `ctx.Logger()` inside Views instead of rebuilding request attributes.
6. Configure realtime metrics separately with `realtime.Options{Recorder: recorder}`.
7. Keep metric attributes bounded: route patterns and fixed outcome vocabularies only; never raw URLs, IDs, tokens, or error messages.
8. Remember that router-generated 404/405 responses are not observed in this version.

Canonical explanation and examples: `docs/guides/observability.md`. Public symbols: `docs/reference.md`.

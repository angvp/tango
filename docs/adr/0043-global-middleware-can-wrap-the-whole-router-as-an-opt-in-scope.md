# Global middleware can wrap the whole router as an opt-in scope

`Config.Middleware` has always wrapped each matched route, not the router. So an Unmatched request, one the router answers itself with 404 or 405, skipped every global middleware: `RequestID`, `AccessLogger`, `Recoverer`, a body limit, and automatic HTTP metrics. Production 404s and 405s went unlogged and uncounted.

A new `Config.MiddlewareScope` decides what global middleware wraps:
- `MiddlewareScopeRoutes` wraps each matched route, as before.
- `MiddlewareScopeAll` wraps the whole router, Unmatched requests included.
- The zero value, `MiddlewareScopeDefault`, means the framework's default, which is `MiddlewareScopeRoutes` for now. Projects created by `tango newproject` set `MiddlewareScopeAll`.

How `MiddlewareScopeAll` works:
- **Route identity is resolved first.** tanGO resolves the request's route before any global middleware runs, without invoking a handler, so every layer reports the same route. A matched request reports its route's pattern, and an Unmatched request reports `"(unmatched)"`.
- **Rewriting a route is unsupported.** Middleware that rewrites the method or path to make the router dispatch elsewhere is not supported.
- **Metrics stay outermost and run once.** A matched request's metrics and logs are identical under both scopes, including requests global middleware rejects before routing.

Optional `Config.NotFound` and `Config.MethodNotAllowed` Views replace the router's responses under either scope. They run through the ordinary View terminal; under `MiddlewareScopeRoutes` they run outside global middleware and are not observed.

**Why opt-in, not a new default.** This is the first behaviour change since v0.1.0's compatibility promise ([ADR 0042](0042-v0-1-covers-every-exported-api-except-named-exclusions.md)). Running global middleware on requests it never saw changes existing apps' responses: an authentication middleware, for example, would turn a 404 into a redirect. So the change ships as an opt-in, and new projects opt in. The default may flip only after a minor release of notice in the changelog. An app can pin either behaviour explicitly, because the zero value means "the default" rather than either scope.

Rejected:

- **Changing the default in place.** It's a behaviour change to the Covered API with no notice, and the first release under the compatibility promise would break it.
- **Observing Unmatched requests through framework metrics only.** Access logs would still miss them, because `AccessLogger` is global middleware the app adds itself.
- **A boolean opt-in.** Its zero value would have to mean the old behaviour forever, so the default could never flip. It also couldn't express "pin today's behaviour" explicitly.
- **Letting route-level middleware see Unmatched requests.** An Unmatched request has no route, so only the global tier can apply.

Consequences:
- **Precedent.** Behaviour changes ship opt-in first, and a default changes only after a minor release of notice. `docs/compatibility.md` states this rule.
- **The route value is stable.** `"(unmatched)"` joins the Covered API's observability names.
- **Pattern resolution happens twice.** Under `MiddlewareScopeAll`, every request's route pattern is resolved once before middleware and once during dispatch.

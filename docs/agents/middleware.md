# Agent Recipe: Middleware and View Wrappers

Use this when adding cross-cutting request behavior or protecting routes.

Canonical files: `middleware.go`, `routeregistry.go`, `accounts/require_login.go`, and `auth/session.go`. Runnable proof: `examples/board/main.go` (global middleware with `MaxBodySize` and `MiddlewareScopeAll`). Human guide: `docs/guides/routing-and-reverse-lookup.md`.

## Rule

- `tango.Middleware` is raw HTTP middleware: `func(http.Handler) http.Handler`. It runs before `*tango.Context` exists.
- A View wrapper is the `tango.View` composition idiom. It is `*tango.Context`-aware and is the right layer for auth redirects, current-user checks, and permission-like app logic.

## Use Middleware For

- Panic recovery with `tango.Recoverer()`.
- Correlation IDs with `tango.RequestID()` and structured access events with `tango.AccessLogger()`.
- CORS, compression, security headers, or other raw `net/http` concerns.

- Request body limits with `tango.MaxBodySize(n)` (`n` must be positive; it panics otherwise). The most restrictive limit on a request wins: a limit closer to the View can lower an outer limit, never raise it. A route that needs larger bodies must be left outside the global limit, for example in a separately mounted group with its own `WithMiddleware(tango.MaxBodySize(...))`.

When using all three observability middleware, order them `RequestID -> Recoverer -> AccessLogger`. See `docs/agents/observability.md`.

Under `MiddlewareScopeAll`, global middleware runs before the router dispatches:

- It cannot read route parameters (`chi.URLParam` and friends are empty). Read them in group or route middleware, or in the View.
- It must not rewrite the method or path to change which route answers; tanGO resolves the route from the request as it arrived, before global middleware.

Attachment tiers:

- `tango.Config.Middleware` wraps every matched route, or the whole router (Unmatched 404/405 requests included) when `Config.MiddlewareScope` is `tango.MiddlewareScopeAll`. The zero value, `MiddlewareScopeDefault`, means `MiddlewareScopeRoutes` for now; set a scope explicitly to pin the behaviour.
- `tango.WithMiddleware(...)` on `Routes().Include(...)` wraps one group/prefix.
- `tango.Use(...)` on `tango.Path(...)` wraps one route.

## Use View Wrappers For

- `accounts.RequireLogin` or `auth.RequireLogin`.
- Checks that need `ctx.Param`, `ctx.Query`, `ctx.Redirect`, app sessions, or app-owned user data.

Tiny shape:

```go
tango.Path("GET", "/dashboard/", accounts.RequireLogin(store, cookieName, "/accounts/login/", Dashboard))
```

## Don't

- Do not try to use `*tango.Context` helpers inside `tango.Middleware`; it does not exist yet.
- Do not add a route-level `MaxBodySize` expecting it to raise a global limit.
- Do not reimplement raw HTTP concerns as View wrappers when standard Go middleware already fits.

## Check

- Compare middleware implementation with `middleware.go`.
- Compare auth View-wrapper behavior with `accounts/require_login.go`.
- Run the shared checklist: `docs/agents/checklist.md`.

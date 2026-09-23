# Agent Recipe: Middleware and View Wrappers

Use this when adding cross-cutting request behavior or protecting routes.

Canonical files: `middleware.go`, `routeregistry.go`, `accounts/require_login.go`, and `auth/session.go`.

## Rule

- `tango.Middleware` is raw HTTP middleware: `func(http.Handler) http.Handler`. It runs before `*tango.Context` exists.
- A View wrapper is the `tango.View` composition idiom. It is `*tango.Context`-aware and is the right layer for auth redirects, current-user checks, and permission-like app logic.

## Use Middleware For

- Panic recovery with `tango.Recoverer()`.
- Correlation IDs with `tango.RequestID()` and structured access events with `tango.AccessLogger()`.
- CORS, compression, security headers, or other raw `net/http` concerns.

When using all three observability middleware, order them `RequestID -> Recoverer -> AccessLogger`. See `docs/agents/observability.md`.

Attachment tiers:

- `tango.Config.Middleware` wraps every route.
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
- Do not reimplement raw HTTP concerns as View wrappers when standard Go middleware already fits.

## Check

- Compare middleware implementation with `middleware.go`.
- Compare auth View-wrapper behavior with `accounts/require_login.go`.
- Run the shared checklist: `docs/agents/checklist.md`.

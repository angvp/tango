# Guide: routing and reverse lookup

## Declaring routes

`Include` mounts a list of `Route` values under a prefix:

```go
registry.Routes().Include("/posts/", tango.URLs{
	tango.Path("GET", "/", listPosts, tango.Name("list")),
	tango.Path("GET", "/{id}/", getPost, tango.Name("detail")),
})
```

`Path(method, pattern, view, opts...)` builds a route declaration; it does not run anything. `tango.Name("list")` attaches a name. Path patterns use chi's `{param}` syntax directly — chi itself is never part of your imports (it's an internal implementation detail).

`Include("/posts/", ...)` and `Include("/posts", ...)` behave identically; trailing slashes are normalized.

## Middleware vs View wrappers

tanGO has two wrapper mechanisms at different layers.

`Middleware` is the standard Go shape:

```go
type Middleware func(http.Handler) http.Handler
```

It runs at the raw `net/http` layer before `*tango.Context` exists. Use it for cross-cutting HTTP concerns that do not need tanGO's `Context`: panic recovery, request IDs, access logging, CORS, compression, security headers, rate limiting, and similar behavior. Middleware can pass data forward with Go's standard request context (`r.WithContext(...)`), and a View can read it back through `ctx.Request().Context()`.

Middleware can be attached at three tiers:

```go
config := tango.Config{
	Middleware: []tango.Middleware{
		tango.Recoverer(),
		myAccessLogger,
	},
}

registry.Routes().Include("/admin/", adminURLs,
	tango.WithMiddleware(adminHeaders),
)

tango.Path("GET", "/checkout/", checkout,
	tango.Use(rateLimitCheckout),
)
```

Composition order is always outer to inner:

```text
Config.Middleware -> Include WithMiddleware -> Path Use -> View
```

### Middleware scope

By default, `Config.Middleware` wraps each matched route. A request no route matches, an **Unmatched request**, gets the router's `404`, or `405` when the path exists but not with that method. It passes none of your global middleware, so it isn't logged, counted, recovered or body-limited. Set `Config.MiddlewareScope` to `tango.MiddlewareScopeAll` to wrap the whole router instead:

```go
config := tango.Config{
	Middleware:      []tango.Middleware{tango.RequestID(), tango.Recoverer(), tango.AccessLogger()},
	MiddlewareScope: tango.MiddlewareScopeAll,
}
```

Under `MiddlewareScopeAll`:

- **Route resolved first:** tanGO resolves each request's route before global middleware runs, from the request as it arrived. Logs, metrics and `ctx.Logger()` report that route's pattern, or `"(unmatched)"` for a `404` or `405`. A request a global middleware answers before routing, such as a rate limit's `429` or a body limit's `413`, still reports the route it matches.
- **Once per request:** global middleware runs once per request, around the router. Group and route middleware still run inside it, only for their routes.
- **No route rewriting:** middleware that rewrites the request's method or path so the router dispatches it to a different route is not supported. The route was already resolved from the original request.

`tango newproject` sets `MiddlewareScopeAll`. The zero value, `MiddlewareScopeDefault`, means the framework's default, currently `MiddlewareScopeRoutes`. See [ADR 0043](../adr/0043-global-middleware-can-wrap-the-whole-router-as-an-opt-in-scope.md).

### Custom 404 and 405 pages

`Config.NotFound` and `Config.MethodNotAllowed` are optional Views that answer Unmatched requests in place of the router's responses: `NotFound` when no route matches the path, `MethodNotAllowed` when a route matches the path but not the method.

```go
config := tango.Config{
	NotFound: func(ctx *tango.Context) error {
		return ctx.HTML(http.StatusNotFound, pages, "not_found.html", nil)
	},
}
```

- **Like any View:** both run like any other View, so an error they return gets the generic `500`, and the error is logged with route `"(unmatched)"`.
- **The `Allow` header:** before `MethodNotAllowed` runs, tanGO sets `Allow` to the methods the path does accept.
- **Unset:** a View left unset keeps the router's own response, a plain-text `404 page not found`, or an empty `405` with `Allow`.

They work under either [middleware scope](#middleware-scope), but the scope decides what wraps them:

- **Under `MiddlewareScopeAll`,** they run inside `Config.Middleware`, and are logged and counted as route `"(unmatched)"`.
- **Under `MiddlewareScopeRoutes`,** they run outside `Config.Middleware`: no `RequestID`, no access log, no automatic metrics. A `Recoverer` configured only in `Config.Middleware` does not protect them either, so a panic in one is not recovered. Keep them simple, or use `MiddlewareScopeAll`.

Built-in middleware remains opt-in. `tango.Recoverer()` catches downstream panics and returns tanGO's generic JSON `500`; `tango.RequestID()` adds correlation IDs; `tango.AccessLogger()` emits structured access events. When all three are used, order them `RequestID -> Recoverer -> AccessLogger`. See [structured logging and observability](observability.md).

### Request body limits

`tango.MaxBodySize(n)` caps request bodies at `n` bytes, at any tier:

```go
config := tango.Config{
	Middleware: []tango.Middleware{
		tango.MaxBodySize(1 << 20), // 1 MiB for every route
	},
}
```

- **Declared too large:** a request whose `Content-Length` already exceeds the limit gets `413` with `{"error":"request body too large"}` before its View runs.
- **Undeclared size:** for a body whose size isn't declared (a chunked upload, say), the limit applies as the body is read. If `ctx.Bind`, a form parse or any other read goes past it, the read fails with an `*http.MaxBytesError`. When the View returns that error, tanGO answers with the same `413`, not the generic `500`, and doesn't log it as a View error.
- **Handled by the View:** a View that checks for the error with `errors.As` and answers itself keeps control of its response.
- **Invalid size:** `n` must be positive, and `MaxBodySize` panics otherwise.

**The most restrictive limit wins.** Middleware runs outermost first, so a limit at an inner tier can keep or lower an outer one, never raise it. A `tango.Use(tango.MaxBodySize(50 << 20))` on an upload route does nothing when `Config.Middleware` already caps bodies at 1 MiB: the global limit sees the body first. If an app needs large uploads, don't set the body limit globally. Apply `MaxBodySize` instead to the route groups (`Include` with `tango.WithMiddleware`) or routes that should stay limited, and give the upload route its own, larger limit.

A **View wrapper** is different: it is a `func(tango.View) tango.View` that runs after `*tango.Context` exists. Use View wrappers for Context-aware behavior such as auth, permissions, current-user lookup, redirects, and admin session checks. `auth.RequireLogin(...)` is a View wrapper, not middleware. Middleware and View wrappers permanently coexist; neither replaces the other.

## Namespace-qualified names

A route's name is always qualified by its `Include` prefix: the routes above are `posts:list` and `posts:detail`, not `list`/`detail`. There is no shorthand/local-name form — every reverse lookup uses the fully-qualified name. This means two different apps can each have a route named `list` without colliding: `posts:list` and `comments:list` are distinct.

`Include` fails fast (registering none of its routes) on a malformed pattern or a duplicate qualified name *within that call*. A duplicate qualified name *across* different `Include` calls (e.g. two apps both producing `posts:list`) is only caught later, when `Handler()`/`Reverser()` compile the full route tree — which requires `Registry.RunRegistration()` to have completed.

## Reverse lookup

```go
reverser, err := registry.Routes().Reverser()
url, err := reverser.Reverse("posts:detail", tango.Params{"id": "42"})
// url == "/posts/42/"
```

`Reverse` fails with `ErrUnknownRouteName` for a name that was never registered, and `ErrMissingParam` if a pattern placeholder has no corresponding entry in `params`. Build the `Reverser` once (it's a full route-tree compile) and reuse it — don't call `Reverser()` per request.

## Error handling

A view returning a non-nil error results in a fixed 500 response with a generic JSON body; the underlying error is logged server-side but never sent to the client. The one exception is a [body limit](#request-body-limits)'s `*http.MaxBytesError`, which gets `413` instead. There's no per-route custom error-handling hook — return a JSON error body yourself (via `ctx.JSON`) for anything a client needs to see.

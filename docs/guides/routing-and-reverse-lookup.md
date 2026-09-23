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

Built-in middleware remains opt-in. `tango.Recoverer()` catches downstream panics and returns tanGO's generic JSON `500`; `tango.RequestID()` adds correlation IDs; `tango.AccessLogger()` emits structured access events. When all three are used, order them `RequestID -> Recoverer -> AccessLogger`. See [structured logging and observability](observability.md).

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

A view returning a non-nil error results in a fixed 500 response with a generic JSON body; the underlying error is logged server-side but never sent to the client. There's no per-route custom error-handling hook in v0.0.1 — return a JSON error body yourself (via `ctx.JSON`) for anything a client needs to see.

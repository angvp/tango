# Agent Recipe: Routing and Reverse Lookup

Use this when exposing a View over HTTP, mounting an app, or avoiding hard-coded internal URLs.

Canonical example: `examples/api-with-admin/apps/posts/app.go`. Human guide: `docs/guides/routing-and-reverse-lookup.md`.

## Build

- Write a `tango.View`: `func(ctx *tango.Context) error`.
- Declare each endpoint with `tango.Path(method, pattern, view, opts...)`.
- Mount an app's routes once from `App.Register` with `registry.Routes().Include(prefix, urls)`.
- Use `{name}` path parameters and read them with `ctx.Param("name")`.
- Give any URL that another View or template must build a `tango.Name("...")`; resolve it through `Reverser.Reverse`, never by repeating a string literal.

Tiny shape:

```go
var URLs = tango.URLs{
    tango.Path(http.MethodGet, "/books/{id}/", Detail, tango.Name("detail")),
}

func New() tango.App {
    return tango.NewApp("books", func(registry *tango.Registry) error {
        return registry.Routes().Include("/books/", URLs)
    })
}
```

The route name above is `books:detail`, because `Include` namespaces names using its prefix.

## Middleware scope

- `Config.Middleware`: every matched route.
- `tango.WithMiddleware(...)` on `Include`: one mounted group.
- `tango.Use(...)` on `Path`: one endpoint.

They compose outer-to-inner: global → group → route → View. Use `docs/agents/middleware.md` for the middleware/View-wrapper distinction.

## Don't

- Do not import or expose Chi types.
- Do not mount routes from `init` or by scanning directories.
- Do not use raw request paths as route names; name the route and reverse it when another part of the app owns the link.
- Do not expect router-generated 404/405 responses to run route middleware in this version.

## Check

- Confirm every `Include` returns its error from `Register`.
- Exercise a path parameter and reverse every named route used by the app.
- Run `docs/agents/checklist.md`.

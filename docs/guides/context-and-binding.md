# Guide: `Context` responses and request binding

Every `View` is `func(*tango.Context) error`. `Context` wraps one request/response pair.

## Reading the request

- **`Param(name string) string`** — a named path parameter (from the route pattern's `{name}` placeholders), or `""` if absent.
- **`Query(name string) string`** — a named URL query parameter, or `""` if absent.
- **`Bind(dst any) error`** — decodes the request body as JSON into `dst`. There is no other content-type support (form values, multipart) in v0.1; read `ctx.Request()` directly for that.

## Writing the response

- **`JSON(status int, payload any) error`** — sets `Content-Type: application/json`, writes `status`, and JSON-encodes `payload`.
- **`Redirect(url string) error`** — writes an HTTP redirect (`302 Found`) to `url`.

## Escape hatches

- **`Request() *http.Request`** and **`ResponseWriter() http.ResponseWriter`** — the raw request/response pair, for anything the helpers above don't cover (custom headers, streaming, cookies).
- **`Context() context.Context`** — the wrapped request's standard `context.Context` (for cancellation, deadlines, and passing to `db.Store` methods, which all take a `context.Context` as their first argument). Note `*tango.Context` itself does **not** implement `context.Context` — call `ctx.Context()` explicitly wherever a `context.Context` is expected.

```go
func createPost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var post Post
		if err := ctx.Bind(&post); err != nil {
			return err
		}
		if err := store.Create(ctx.Context(), meta, &post); err != nil {
			return err
		}
		return ctx.JSON(201, post)
	}
}
```

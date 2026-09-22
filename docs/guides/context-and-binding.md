# Guide: `Context` responses and request binding

Every `View` is `func(*tango.Context) error`. `Context` wraps one request/response pair.

## Reading the request

- **`Param(name string) string`** — a named path parameter (from the route pattern's `{name}` placeholders), or `""` if absent.
- **`Query(name string) string`** — a named URL query parameter, or `""` if absent.
- **`Bind(dst any) error`** — decodes the request body as JSON into `dst`. There is no other content-type support (form values, multipart) in v0.0.1; read `ctx.Request()` directly for that.

## Writing the response

- **`JSON(status int, payload any) error`** — sets `Content-Type: application/json`, writes `status`, and JSON-encodes `payload`.
- **`Redirect(url string) error`** — writes an HTTP redirect (`302 Found`) to `url`.
- **`HTML(status int, tmpl *template.Template, name string, data any) error`** — renders `tmpl`'s named template `name` (via `tmpl.ExecuteTemplate`) against `data` and writes it as `text/html; charset=utf-8`. The render is buffered: nothing reaches the response unless execution fully succeeds, so a template error comes back as an ordinary returned `error` — the same fixed-500 path any other view's error takes — rather than a response that already committed a status code followed by a truncated or malformed body.

## Server-rendered HTML apps

`HTML` has no opinion on where templates come from or how they're parsed — that's ordinary Go, not a tanGO concern. A typical shape: embed templates once at package init, parse them into one `*template.Template`, and call `ctx.HTML` per request with whichever named template that request needs:

```go
//go:embed templates/*.gohtml
var templatesFS embed.FS

var tmpl = template.Must(template.ParseFS(templatesFS, "templates/*.gohtml"))

func bookDetail(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var book Book
		if err := store.Get(ctx.Context(), meta, ctx.Param("id"), &book); err != nil {
			return err
		}
		return ctx.HTML(http.StatusOK, tmpl, "book_detail", book)
	}
}
```

A shared base layout composes the same way any `html/template` set does — a `{{define "layout"}}...{{end}}` template that other defined templates invoke via `{{template "layout" .}}`, all parsed together into the one `*template.Template` passed to every `ctx.HTML` call.

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

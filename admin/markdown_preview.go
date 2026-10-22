package admin

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/internal/security"
	"github.com/angvp/tango/model"
)

// previewInputLimit caps the source text one preview request may send, in
// bytes of the request body. A larger body is refused with 413 before any of
// it is kept.
const previewInputLimit = 256 << 10

// previewOutputLimit caps the HTML a host renderer may return. It limits
// the response; it cannot stop a renderer that allocates without returning.
const previewOutputLimit = 1 << 20

// previewCSRFHeader carries the CSRF token on a preview request. The source
// travels as the raw body, so the size cap applies to the source itself.
const previewCSRFHeader = "X-CSRF-Token"

// previewTimeout bounds one renderer call. The deadline is cooperative: a
// renderer that ignores its context keeps running, and admin does not start
// a goroutine to return early.
var previewTimeout = 5 * time.Second

// previewDocumentStart and previewDocumentEnd wrap a successful render in a
// complete document. The CSP meta is the only policy and is pinned by tests: the preview has no scripts,
// no styles, no forms and no outbound loads, and a base URL cannot be set.
const previewDocumentEnd = "\n</body>\n</html>\n"

// previewFailure is a preview request that did not produce HTML: the status
// to answer with and the message the client may show. Like the other JSON
// the admin answers with, the message stays English: clients branch on the
// status, and the page's own text is what is translated.
type previewFailure struct {
	Status  int
	Message string
}

// previewRoutes returns the one preview route a model needs: a POST to
// modelPath + "_preview/{field}/". It is registered only when a field of the
// model has a PreviewWidget. The field is looked up after the session check,
// so an anonymous request learns nothing about which fields have previews.
// modelAdminPath is the admin base path of a model, with a trailing slash.
func modelAdminPath(modelName string) string {
	return "/admin/" + db.ColumnName(modelName) + "/"
}

// previewPath is the canonical URL of a model field's preview route. The
// form hands it to the widget, and previewRoutes registers exactly it.
func previewPath(modelName, field string) string {
	return modelAdminPath(modelName) + "_preview/" + field + "/"
}

func previewRoutes(modelPath string, store *db.Store, registration ModelRegistration) tango.URLs {
	if !hasPreviewWidget(registration.Options) {
		return nil
	}
	view := previewView(registration.Model, registration.Options)
	return tango.URLs{
		tango.Path(http.MethodPost, modelPath+"_preview/{field}/", requireSession(store, view), hiddenPage),
	}
}

func hasPreviewWidget(opts adminregistry.Options) bool {
	for _, widget := range opts.Widgets {
		if _, ok := widget.(PreviewWidget); ok {
			return true
		}
	}
	return false
}

// previewView answers a preview request for the field named in the path. It
// is 404 for an unknown field, a primary key, a read-only field, or a field
// whose widget is not a PreviewWidget.
func previewView(meta model.ModelMeta, opts adminregistry.Options) tango.View {
	return func(ctx *tango.Context) error {
		setPreviewHeaders(ctx)
		preview, ok := previewWidgetFor(meta, opts, ctx.Param("field"))
		if !ok {
			return previewError(ctx, http.StatusNotFound, "not found")
		}
		if !verifyPreviewCSRF(ctx) {
			return previewError(ctx, http.StatusForbidden, "invalid or missing CSRF token")
		}
		source, failure := readPreviewSource(ctx)
		if failure != nil {
			return previewError(ctx, failure.Status, failure.Message)
		}
		html, failure := renderPreview(ctx, meta, ctx.Param("field"), preview, source)
		if failure != nil {
			return previewError(ctx, failure.Status, failure.Message)
		}
		return ctx.JSON(http.StatusOK, map[string]string{"html": previewDocument(ctx.Context(), html)})
	}
}

// previewWidgetFor returns the field's widget when it is an editable,
// non-primary-key field whose widget can preview.
func previewWidgetFor(meta model.ModelMeta, opts adminregistry.Options, name string) (PreviewWidget, bool) {
	if contains(opts.ReadOnly, name) {
		return nil, false
	}
	for _, field := range meta.Fields {
		if field.Name != name || field.PrimaryKey {
			continue
		}
		preview, ok := opts.Widgets[name].(PreviewWidget)
		return preview, ok
	}
	return nil, false
}

// verifyPreviewCSRF reports whether the request's X-CSRF-Token matches the
// token the session's forms carry.
func verifyPreviewCSRF(ctx *tango.Context) bool {
	cookie, err := ctx.Request().Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return security.VerifyCSRFToken(ctx.Request().Header.Get(previewCSRFHeader), cookie.Value)
}

// readPreviewSource reads the request body up to previewInputLimit. The
// source must be valid UTF-8; a body over the limit is 413 and any other
// read failure is 400.
func readPreviewSource(ctx *tango.Context) (string, *previewFailure) {
	body := http.MaxBytesReader(ctx.ResponseWriter(), ctx.Request().Body, previewInputLimit)
	raw, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", &previewFailure{http.StatusRequestEntityTooLarge, "request body too large"}
		}
		return "", &previewFailure{http.StatusBadRequest, "could not read the preview text"}
	}
	if !utf8.Valid(raw) {
		return "", &previewFailure{http.StatusBadRequest, "preview text must be UTF-8"}
	}
	return string(raw), nil
}

// renderPreview runs the host callback under previewTimeout. It logs the
// detail of a failure and returns only a status and a short message. A
// renderer that finishes after its deadline counts as timed out.
func renderPreview(ctx *tango.Context, meta model.ModelMeta, field string, preview PreviewWidget, source string) (template.HTML, *previewFailure) {
	renderCtx, cancel := context.WithTimeout(ctx.Context(), previewTimeout)
	defer cancel()

	html, err := preview.Preview(renderCtx, source)
	if errors.Is(renderCtx.Err(), context.DeadlineExceeded) {
		ctx.Logger().Warn("admin preview timed out", "model", meta.Name, "field", field)
		return "", &previewFailure{http.StatusGatewayTimeout, "the preview took too long to render"}
	}
	if err != nil {
		ctx.Logger().Error("admin preview failed", "model", meta.Name, "field", field, "error", err)
		return "", &previewFailure{http.StatusUnprocessableEntity, "the preview could not be rendered"}
	}
	if len(html) > previewOutputLimit {
		ctx.Logger().Error("admin preview output too large", "model", meta.Name, "field", field, "bytes", len(html))
		return "", &previewFailure{http.StatusUnprocessableEntity, "the preview is too large"}
	}
	return html, nil
}

// previewDocument wraps body in the preview page, in the language and with
// the title of the catalog the request selected.
func previewDocument(ctx context.Context, body template.HTML) string {
	return previewDocumentStart(ctx) + string(body) + previewDocumentEnd
}

func previewDocumentStart(ctx context.Context) string {
	return `<!doctype html>
<html lang="` + template.HTMLEscapeString(pageLang(ctx)) + `">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; base-uri 'none'; form-action 'none'">
<title>` + template.HTMLEscapeString(i18n.T(ctx, "admin.markdown.document_title", "Preview")) + `</title>
</head>
<body>
`
}

// setPreviewHeaders keeps every preview response out of caches and stops a
// browser from sniffing its JSON into another type.
func setPreviewHeaders(ctx *tango.Context) {
	header := ctx.ResponseWriter().Header()
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
}

func previewError(ctx *tango.Context, status int, message string) error {
	return ctx.JSON(status, map[string]string{"error": message})
}

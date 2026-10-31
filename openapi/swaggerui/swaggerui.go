// Package swaggerui serves Swagger UI, embedded in the binary, for the
// openapi package:
//
//	openapi.New(openapi.Options{UI: swaggerui.New()})
//
// Importing it adds about 1.5 MB to the binary, which is why it is a package of
// its own: an app that only wants the document does not import it. Nothing is
// fetched from the network, so the page works offline. Under a strict
// Content-Security-Policy the page needs three relaxations: 'unsafe-inline' in
// style-src, data: in img-src, and 'self' in connect-src so it can fetch the
// document. WithContentSecurityPolicy grants them on the page's route only.
package swaggerui

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/angvp/tango"
)

//go:embed assets/swagger-ui-bundle.js assets/swagger-ui.css
var assets embed.FS

// Version is the swagger-ui-dist release the assets come from.
const Version = "5.17.14"

// UI is the Swagger UI page and its files.
type UI struct {
	policy *tango.CSP
}

// Option configures New.
type Option func(*UI)

// New returns the embedded Swagger UI.
func New(opts ...Option) UI {
	var ui UI
	for _, opt := range opts {
		opt(&ui)
	}
	return ui
}

// WithContentSecurityPolicy serves the UI page under app with three changes:
// style-src is 'self' and 'unsafe-inline', because Swagger UI writes inline
// styles at runtime; img-src is app's effective img-src with data: added,
// because its styles reference data: images; and connect-src is app's
// effective connect-src with 'self' added, so the page can fetch its own
// document. An absent directive falls back to default-src, and a directive
// that restricts nothing is left unrestricted. Every other directive of app
// is kept, so the relaxation is confined to the page's route. The middleware
// sets the page's header in place of the application's. The document, the
// assets and the rest of the application keep app's policy.
func WithContentSecurityPolicy(app tango.CSP) Option {
	return func(ui *UI) {
		relaxed := app
		relaxed.StyleSrc = []tango.Source{tango.Self(), tango.UnsafeInlineStyle()}
		relaxed.ImgSrc = withSource(effectiveSources(app.ImgSrc, app.DefaultSrc), tango.Data())
		relaxed.ConnectSrc = withSource(effectiveSources(app.ConnectSrc, app.DefaultSrc), tango.Self())
		ui.policy = &relaxed
	}
}

// effectiveSources returns the sources a fetch directive resolves to under
// app: its own list, or the default-src list when it has none. It returns nil
// when neither is set, which means the directive is unrestricted.
func effectiveSources(direct, defaults []tango.Source) []tango.Source {
	if len(direct) > 0 {
		return direct
	}
	return defaults
}

// withSource returns effective with want appended once, dropping 'none' so
// the result is not a contradiction. It returns nil when effective is nil,
// because adding want there would restrict a directive the application left
// open.
func withSource(effective []tango.Source, want tango.Source) []tango.Source {
	if len(effective) == 0 {
		return nil
	}
	var sources []tango.Source
	for _, s := range effective {
		if s != tango.None() {
			sources = append(sources, s)
		}
	}
	if !containsSource(sources, want) {
		sources = append(sources, want)
	}
	return sources
}

func containsSource(sources []tango.Source, want tango.Source) bool {
	for _, s := range sources {
		if s == want {
			return true
		}
	}
	return false
}

const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API documentation</title>
<link rel="stylesheet" href="swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="swagger-ui-bundle.js"></script>
<script src="init.js"></script>
</body>
</html>
`

// Routes returns the routes that serve the page under base (a path ending in
// "/", such as "/docs/") for the document at specPath. The openapi package
// calls it.
func (u UI) Routes(base, specPath string) []tango.Route {
	base = "/" + strings.Trim(base, "/")
	if base != "/" {
		base += "/"
	}
	hidden := tango.Doc(tango.RouteDoc{Hidden: true})
	bundle, _ := assets.ReadFile("assets/swagger-ui-bundle.js")
	styles, _ := assets.ReadFile("assets/swagger-ui.css")
	spec, _ := json.Marshal(specPath)
	init := []byte("window.addEventListener('load', function () {\n  window.ui = SwaggerUIBundle({url: " +
		string(spec) + ", dom_id: '#swagger-ui', deepLinking: true});\n});\n")

	pageOpts := []tango.RouteOption{hidden, tango.Name("swagger-ui")}
	if u.policy != nil {
		pageOpts = append(pageOpts, tango.Use(tango.SecurityHeaders(tango.WithContentSecurityPolicy(*u.policy))))
	}
	routes := []tango.Route{
		tango.Path(http.MethodGet, base, file("text/html; charset=utf-8", []byte(page)), pageOpts...),
		tango.Path(http.MethodGet, base+"swagger-ui-bundle.js", file("text/javascript; charset=utf-8", bundle), hidden),
		tango.Path(http.MethodGet, base+"swagger-ui.css", file("text/css; charset=utf-8", styles), hidden),
		tango.Path(http.MethodGet, base+"init.js", file("text/javascript; charset=utf-8", init), hidden),
	}
	if base != "/" {
		routes = append(routes, tango.Path(http.MethodGet, strings.TrimSuffix(base, "/"),
			func(ctx *tango.Context) error { return ctx.Redirect(base) }, hidden))
	}
	return routes
}

func file(contentType string, body []byte) tango.View {
	return func(ctx *tango.Context) error {
		w := ctx.ResponseWriter()
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, err := w.Write(body)
		return err
	}
}

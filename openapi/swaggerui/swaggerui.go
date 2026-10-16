// Package swaggerui serves Swagger UI, embedded in the binary, for the
// openapi package:
//
//	openapi.New(openapi.Options{UI: swaggerui.New()})
//
// Importing it adds about 1.5 MB to the binary, which is why it is a package of
// its own: an app that only wants the document does not import it. Nothing is
// fetched from the network, so the page works offline and under a strict
// Content-Security-Policy that allows same-origin scripts and styles (Swagger
// UI sets inline styles, so style-src needs 'unsafe-inline').
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
type UI struct{}

// New returns the embedded Swagger UI.
func New() UI { return UI{} }

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
func (UI) Routes(base, specPath string) []tango.Route {
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

	routes := []tango.Route{
		tango.Path(http.MethodGet, base, file("text/html; charset=utf-8", []byte(page)), hidden, tango.Name("swagger-ui")),
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

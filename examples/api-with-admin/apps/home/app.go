// Package home serves the example's one HTML page, which shows how an
// application template receives the request's Content-Security-Policy nonce.
package home

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
)

// page is parsed once. NonceAttr is registered here because the framework
// does not change a host's templates.
var page = template.Must(template.New("home").Funcs(template.FuncMap{
	"NonceAttr": tango.NonceAttr,
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API with admin</title>
<style{{NonceAttr .Nonce}}>body { font-family: system-ui, sans-serif; margin: 2rem; }</style>
</head>
<body>
<h1>API with admin</h1>
<p><a href="/admin/">Admin</a> &middot; <a href="/posts/">Posts (JSON)</a></p>
</body>
</html>
`))

type pageData struct {
	Nonce string
}

// New constructs the "home" app with GET /.
func New() tango.App {
	return tango.NewApp("home", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/", show, tango.Name("home")),
		})
	})
}

func show(ctx *tango.Context) error {
	return ctx.HTML(http.StatusOK, page, "home", pageData{
		Nonce: tango.CSPNonce(ctx.Context()),
	})
}

package admin

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
)

const pageSize = 25

var listTemplate = cloneWithContent(`{{define "content"}}
<h1>{{.ModelName}}</h1>
<form method="get">
<input type="text" name="q" value="{{.SearchQuery}}" placeholder="Search">
<button type="submit">Search</button>
</form>
<p><a href="{{.BasePath}}new/">New</a></p>
<table>
<thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}<th></th></tr></thead>
<tbody>
{{range .Rows}}<tr>{{range .Values}}<td>{{.}}</td>{{end}}<td>
<a href="{{$.BasePath}}{{.PK}}/">Edit</a>
<a href="{{$.BasePath}}{{.PK}}/delete/">Delete</a>
</td></tr>{{end}}
</tbody>
</table>
{{if .HasPrev}}<a href="?page={{.PrevPage}}&q={{.SearchQuery}}">Previous</a>{{end}}
{{if .HasNext}}<a href="?page={{.NextPage}}&q={{.SearchQuery}}">Next</a>{{end}}
{{end}}`)

var formTemplate = cloneWithContent(`{{define "content"}}
<h1>{{.Title}}</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post">
{{range .Fields}}<div>
<label>{{.Label}}</label>
{{if eq .InputType "checkbox"}}<input type="checkbox" name="{{.Name}}" {{if .Checked}}checked{{end}}>
{{else}}<input type="{{.InputType}}" name="{{.Name}}" value="{{.Value}}">{{end}}
</div>
{{end}}
<button type="submit">Save</button>
</form>
{{end}}`)

var deleteTemplate = cloneWithContent(`{{define "content"}}
<h1>Delete {{.ModelName}} {{.PK}}?</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post">
<button type="submit">Confirm delete</button>
</form>
{{end}}`)

type listRow struct {
	PK     string
	Values []string
}

type listPageData struct {
	ModelName   string
	BasePath    string
	Columns     []string
	Rows        []listRow
	SearchQuery string
	HasPrev     bool
	HasNext     bool
	PrevPage    int
	NextPage    int
}

type formField struct {
	Name      string
	Label     string
	InputType string
	Value     string
	Checked   bool
}

type formPageData struct {
	Title  string
	Error  string
	Fields []formField
}

type deletePageData struct {
	ModelName string
	PK        string
	Error     string
}

func render(ctx *tango.Context, status int, tmpl *template.Template, data any) error {
	ctx.ResponseWriter().Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.ResponseWriter().WriteHeader(status)
	return tmpl.ExecuteTemplate(ctx.ResponseWriter(), "layout", data)
}

func notFound(ctx *tango.Context) error {
	return ctx.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
}

func methodNotAllowed(ctx *tango.Context) error {
	return ctx.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

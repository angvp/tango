package admin

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
)

const pageSize = 25

var listTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn"}}
    <h1 class="topbar-title">{{.ModelName}}</h1>
  </div>
  <a href="{{.BasePath}}new/" class="btn-primary">+ Add {{.ModelName}}</a>
</header>
<div class="content-body">
  <div class="card">
    <div class="card-header">
      <form method="get" class="flex items-center gap-2 flex-1 min-w-0 w-full sm:w-auto sm:min-w-[220px] sm:max-w-sm">
        <input type="text" name="q" value="{{.SearchQuery}}" placeholder="Search {{.ModelName}}&hellip;" class="input">
        <button type="submit" class="btn-secondary shrink-0">Search</button>
      </form>
    </div>
    {{if .Rows}}
    <div class="table-wrap">
      <table class="table-admin">
        <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}<th class="text-right">Actions</th></tr></thead>
        <tbody>
        {{range .Rows}}<tr>
          {{range .Values}}<td>{{if eq . "true"}}<span class="badge-true">Yes</span>{{else if eq . "false"}}<span class="badge-false">No</span>{{else}}{{.}}{{end}}</td>{{end}}
          <td class="text-right">
            <div class="table-actions">
              <a href="{{$.BasePath}}{{.PK}}/" class="btn-ghost">Edit</a>
              <a href="{{$.BasePath}}{{.PK}}/delete/" class="btn-ghost text-red-600 hover:text-red-700 hover:bg-red-50">Delete</a>
            </div>
          </td>
        </tr>{{end}}
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span>{{.TotalCount}} {{if eq .TotalCount 1}}result{{else}}results{{end}}{{if .SearchQuery}} for &ldquo;{{.SearchQuery}}&rdquo;{{end}}</span>
      {{if gt .TotalPages 1}}
      <nav class="pagination-pages" aria-label="Pagination">
        {{if .HasPrev}}<a href="?page={{.PrevPage}}&q={{.SearchQuery}}" class="page-link" aria-label="Previous page">&larr;</a>{{end}}
        {{range .PageLinks}}{{if .Ellipsis}}<span class="page-ellipsis">&hellip;</span>{{else if .Current}}<span class="page-link page-link-current" aria-current="page">{{.Number}}</span>{{else}}<a href="?page={{.Number}}&q={{$.SearchQuery}}" class="page-link">{{.Number}}</a>{{end}}{{end}}
        {{if .HasNext}}<a href="?page={{.NextPage}}&q={{.SearchQuery}}" class="page-link" aria-label="Next page">&rarr;</a>{{end}}
      </nav>
      {{end}}
    </div>
    {{else}}
    <div class="empty-state">
      <p class="empty-state-title">No {{.ModelName}} rows yet</p>
      <p class="empty-state-body">{{if .SearchQuery}}Nothing matches &ldquo;{{.SearchQuery}}&rdquo;.{{else}}Get started by adding the first one.{{end}}</p>
      {{if not .SearchQuery}}<a href="{{.BasePath}}new/" class="btn-primary mt-3">+ Add {{.ModelName}}</a>{{end}}
    </div>
    {{end}}
  </div>
</div>
{{end}}`)

var formTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn"}}
    <h1 class="topbar-title">{{.Title}}</h1>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-2xl">
    <div class="card-body">
      {{if .Error}}<div class="alert-error">{{.Error}}</div>{{end}}
      <form method="post">
      {{range .Fields}}<div class="field-group">
        {{if eq .InputType "checkbox"}}
        <div class="checkbox-row">
          <input type="checkbox" id="field-{{.Name}}" name="{{.Name}}" {{if .Checked}}checked{{end}} class="checkbox">
          <label for="field-{{.Name}}" class="text-sm font-medium text-slate-700">{{.Label}}</label>
        </div>
        {{else}}
        <label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
        <input id="field-{{.Name}}" type="{{.InputType}}" name="{{.Name}}" value="{{.Value}}" class="input">
        {{end}}
      </div>
      {{end}}
        <div class="flex items-center gap-2 pt-2">
          <button type="submit" class="btn-primary">Save</button>
          <a href="../" class="btn-secondary">Cancel</a>
        </div>
      </form>
    </div>
  </div>
</div>
{{end}}`)

var deleteTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn"}}
    <h1 class="topbar-title">Delete {{.ModelName}}</h1>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      {{if .Error}}<div class="alert-error">{{.Error}}</div>{{end}}
      <p class="text-sm text-slate-700 mb-5">Are you sure you want to delete <span class="font-medium text-slate-900">{{.ModelName}} {{.PK}}</span>? This action cannot be undone.</p>
      <form method="post" class="flex items-center gap-2">
        <button type="submit" class="btn-danger">Confirm delete</button>
        <a href="../" class="btn-secondary">Cancel</a>
      </form>
    </div>
  </div>
</div>
{{end}}`)

type listRow struct {
	PK     string
	Values []string
}

type listPageData struct {
	chrome
	ModelName   string
	BasePath    string
	Columns     []string
	Rows        []listRow
	SearchQuery string
	Page        int
	TotalPages  int
	TotalCount  int
	PageLinks   []pageLink
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
	chrome
	Title  string
	Error  string
	Fields []formField
}

type deletePageData struct {
	chrome
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

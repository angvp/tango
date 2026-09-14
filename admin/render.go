package admin

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

const pageSize = 25

var listTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.ModelName}}</h1>
  </div>
  <a href="{{.BasePath}}new/" class="btn-primary">{{.T "admin.button.add_model" "+ Add %s" .ModelName}}</a>
</header>
<div class="content-body">
  <div class="card">
    <div class="card-header">
      <form method="get" class="flex items-center gap-2 flex-1 min-w-0 w-full sm:w-auto sm:min-w-[220px] sm:max-w-sm">
        <input type="text" name="q" value="{{.SearchQuery}}" placeholder="{{.T "admin.list.search_placeholder" "Search %s…" .ModelName}}" class="input">
        <button type="submit" class="btn-secondary shrink-0">{{.T "admin.button.search" "Search"}}</button>
      </form>
    </div>
    {{if .Rows}}
    <div class="table-wrap">
      <table class="table-admin">
        <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}<th class="text-right">{{.T "admin.list.actions" "Actions"}}</th></tr></thead>
        <tbody>
        {{range .Rows}}<tr>
          {{range .Values}}<td>{{if eq . "true"}}<span class="badge-true">{{$.T "admin.boolean.yes" "Yes"}}</span>{{else if eq . "false"}}<span class="badge-false">{{$.T "admin.boolean.no" "No"}}</span>{{else}}{{.}}{{end}}</td>{{end}}
          <td class="text-right">
            <div class="table-actions">
              <a href="{{$.BasePath}}{{.PK}}/" class="btn-ghost">{{$.T "admin.button.edit" "Edit"}}</a>
              <a href="{{$.BasePath}}{{.PK}}/delete/" class="btn-ghost text-red-600 hover:text-red-700 hover:bg-red-50">{{$.T "admin.button.delete" "Delete"}}</a>
            </div>
          </td>
        </tr>{{end}}
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span>{{.TotalCount}} {{if eq .TotalCount 1}}{{.T "admin.list.result_singular" "result"}}{{else}}{{.T "admin.list.result_plural" "results"}}{{end}}{{if .SearchQuery}} {{.T "admin.list.for_query" "for"}} &ldquo;{{.SearchQuery}}&rdquo;{{end}}</span>
      {{if gt .TotalPages 1}}
      <nav class="pagination-pages" aria-label="{{.T "admin.pagination.label" "Pagination"}}">
        {{if .HasPrev}}<a href="?page={{.PrevPage}}&q={{.SearchQuery}}" class="page-link" aria-label="{{.T "admin.pagination.previous" "Previous page"}}">&larr;</a>{{end}}
        {{range .PageLinks}}{{if .Ellipsis}}<span class="page-ellipsis">&hellip;</span>{{else if .Current}}<span class="page-link page-link-current" aria-current="page">{{.Number}}</span>{{else}}<a href="?page={{.Number}}&q={{$.SearchQuery}}" class="page-link">{{.Number}}</a>{{end}}{{end}}
        {{if .HasNext}}<a href="?page={{.NextPage}}&q={{.SearchQuery}}" class="page-link" aria-label="{{.T "admin.pagination.next" "Next page"}}">&rarr;</a>{{end}}
      </nav>
      {{end}}
    </div>
    {{else}}
    <div class="empty-state">
      <p class="empty-state-title">{{.T "admin.list.empty_title" "No %s rows yet" .ModelName}}</p>
      <p class="empty-state-body">{{if .SearchQuery}}{{.T "admin.list.empty_search" "Nothing matches “%s”." .SearchQuery}}{{else}}{{.T "admin.list.empty_body" "Get started by adding the first one."}}{{end}}</p>
      {{if not .SearchQuery}}<a href="{{.BasePath}}new/" class="btn-primary mt-3">{{.T "admin.button.add_model" "+ Add %s" .ModelName}}</a>{{end}}
    </div>
    {{end}}
  </div>
</div>
{{end}}`)

var formTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.Title}}</h1>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-2xl">
    <div class="card-body">
      {{if .Error}}<div class="alert-error">{{.Error}}</div>{{end}}
      <form method="post">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      {{range .Fields}}<div class="field-group">{{.HTML}}</div>
      {{end}}
        <div class="flex items-center gap-2 pt-2">
          <button type="submit" class="btn-primary">{{.T "admin.button.save" "Save"}}</button>
          <a href="../" class="btn-secondary">{{.T "admin.button.cancel" "Cancel"}}</a>
        </div>
      </form>
    </div>
  </div>
</div>
{{end}}`)

var deleteTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.T "admin.delete.title" "Delete %s" .ModelName}}</h1>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      {{if .Error}}<div class="alert-error">{{.Error}}</div>{{end}}
      <p class="text-sm text-slate-700 mb-5">{{.T "admin.delete.confirm_prefix" "Are you sure you want to delete"}} <span class="font-medium text-slate-900">{{.ModelName}} {{.PK}}</span>? {{.T "admin.delete.confirm_suffix" "This action cannot be undone."}}</p>
      <form method="post" class="flex items-center gap-2">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <button type="submit" class="btn-danger">{{.T "admin.button.confirm_delete" "Confirm delete"}}</button>
        <a href="../" class="btn-secondary">{{.T "admin.button.cancel" "Cancel"}}</a>
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
	Name string
	HTML template.HTML
}

type formPageData struct {
	chrome
	Title     string
	Error     string
	Fields    []formField
	CSRFToken string
}

type deletePageData struct {
	chrome
	ModelName string
	PK        string
	Error     string
	CSRFToken string
}

func render(ctx *tango.Context, status int, tmpl *template.Template, data any) error {
	return ctx.HTML(status, tmpl, "layout", data)
}

func notFound(ctx *tango.Context) error {
	return ctx.JSON(http.StatusNotFound, map[string]string{"error": i18n.T(ctx.Context(), "admin.error.not_found", "not found")})
}

func methodNotAllowed(ctx *tango.Context) error {
	return ctx.JSON(http.StatusMethodNotAllowed, map[string]string{"error": i18n.T(ctx.Context(), "admin.error.method_not_allowed", "method not allowed")})
}

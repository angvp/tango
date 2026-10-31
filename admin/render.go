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
  <div class="inline-row">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.ModelName}}</h1>
  </div>
  <a href="{{.BasePath}}new/" class="btn-primary">{{.T "admin.button.add_model" "+ Add %s" .ModelName}}</a>
</header>
<div class="content-body">
  {{if .Deleted}}<div class="alert-success" role="status">{{if eq .Deleted 1}}{{.T "admin.bulk.deleted_one" "Deleted 1 row."}}{{else}}{{.T "admin.bulk.deleted_many" "Deleted %d rows." .Deleted}}{{end}}</div>{{end}}
  <div class="card">
    <div class="card-header">
      <form method="get" class="flex flex-wrap items-end gap-3 w-full">
        <div class="flex flex-col gap-1 flex-1 min-w-[200px]">
          <label for="list-search" class="field-caption">{{.T "admin.list.search_label" "Search"}}</label>
          <input id="list-search" type="text" name="q" value="{{.SearchQuery}}" placeholder="{{.T "admin.list.search_placeholder" "Search %s…" .ModelName}}" class="input">
        </div>
        {{range .Filters}}<div class="flex flex-col gap-1">
          {{if .Options}}<label for="{{.ID}}" class="field-caption">{{.Label}}</label>
          <select id="{{.ID}}" name="{{.Key}}" class="input">{{range .Options}}<option value="{{.Value}}"{{if .Selected}} selected{{end}}>{{.Text}}</option>{{end}}</select>{{else}}<span class="field-caption">{{.Label}}</span>{{end}}
          {{if .Applied}}<input type="hidden" name="{{.Key}}" value="{{.AppliedValue}}"><p class="text-xs text-ink-muted">{{.Applied}}</p>{{end}}
          {{if .Message}}<p class="hint-small">{{.Message}}</p>{{end}}
        </div>{{end}}
        <button type="submit" class="btn-secondary shrink-0">{{.T "admin.button.search" "Search"}}</button>
      </form>
    </div>
    {{if .Rows}}
    {{if .BulkDelete}}<div data-bulk-delete data-msg-selected="{{.T "admin.bulk.selected" "{count} selected"}}" data-msg-title="{{.T "admin.bulk.modal_title" "Delete {count} selected rows?"}}">
    <div class="flex items-center gap-3 border-b border-line-subtle px-5 py-3" data-bulk-bar hidden>
      <button type="button" class="btn-danger" data-bulk-open disabled>{{.T "admin.bulk.delete_selected" "Delete selected"}}</button>
      <span class="text-sm text-ink-muted" data-bulk-count aria-live="polite"></span>
    </div>
    <dialog class="m-auto w-[calc(100%-2rem)] max-w-lg rounded-xl border border-line bg-surface p-0 text-ink shadow-lg backdrop:bg-scrim/60" data-bulk-dialog aria-labelledby="bulk-title">
      <form method="post" action="{{.BasePath}}delete-selected/" class="p-5" data-bulk-form>
        <input type="hidden" name="csrf_token" value="{{.SignedIn.CSRFToken}}">
        <input type="hidden" name="return_to" value="{{.ReturnTo}}">
        <input type="hidden" name="count" value="" data-bulk-form-count>
        <h2 id="bulk-title" class="text-base font-semibold text-ink" data-bulk-title></h2>
        <ul class="my-3 max-h-48 list-disc overflow-y-auto pl-5 text-sm text-ink-muted" data-bulk-list></ul>
        <p class="mb-5 text-sm text-ink-muted">{{.T "admin.bulk.cascade_warning" "Rows that reference these rows are deleted with them. This action cannot be undone."}}</p>
        <div class="inline-row">
          <button type="submit" class="btn-danger" data-bulk-confirm>{{.T "admin.button.confirm_delete" "Confirm delete"}}</button>
          <button type="button" class="btn-secondary" data-bulk-cancel autofocus>{{.T "admin.button.cancel" "Cancel"}}</button>
        </div>
      </form>
    </dialog>{{end}}
    <div class="table-wrap">
      <table class="table-admin">
        <caption class="sr-only">{{.ModelName}}</caption>
        <thead><tr>{{if .BulkDelete}}<th scope="col" data-bulk-cell hidden><input type="checkbox" data-bulk-select-all aria-label="{{.T "admin.bulk.select_all" "Select all %d rows on this page" (len .Rows)}}"></th>{{end}}{{range .Columns}}<th scope="col">{{.}}</th>{{end}}<th scope="col" class="text-right">{{.T "admin.list.actions" "Actions"}}</th></tr></thead>
        <tbody>
        {{range .Rows}}<tr>
          {{if $.BulkDelete}}<td data-bulk-cell hidden><input type="checkbox" data-bulk-pk="{{.PK}}" data-bulk-identity="{{.Identity}}" aria-label="{{$.T "admin.bulk.select_row" "Select %s" .Identity}}"></td>{{end}}
          {{range .Cells}}<td>{{if .Href}}<a href="{{.Href}}" rel="noopener noreferrer">{{.Text}}</a>{{else if eq .Badge "yes"}}<span class="badge-true">{{$.T "admin.boolean.yes" "Yes"}}</span>{{else if eq .Badge "no"}}<span class="badge-false">{{$.T "admin.boolean.no" "No"}}</span>{{else}}{{.Text}}{{end}}</td>{{end}}
          <td class="text-right">
            <div class="table-actions">
              <a href="{{$.BasePath}}{{.PK}}/" class="btn-ghost">{{$.T "admin.button.edit" "Edit"}}</a>
              <a href="{{$.BasePath}}{{.PK}}/delete/" class="btn-ghost text-danger-fg hover:bg-danger-bg">{{$.T "admin.button.delete" "Delete"}}</a>
            </div>
          </td>
        </tr>{{end}}
        </tbody>
      </table>
    </div>
    {{if .BulkDelete}}</div>{{end}}
    <div class="pagination">
      <span>{{.TotalCount}} {{if eq .TotalCount 1}}{{.T "admin.list.result_singular" "result"}}{{else}}{{.T "admin.list.result_plural" "results"}}{{end}}{{if .SearchQuery}} {{.T "admin.list.for_query" "for"}} &ldquo;{{.SearchQuery}}&rdquo;{{end}}</span>
      {{if gt .TotalPages 1}}
      <nav class="pagination-pages" aria-label="{{.T "admin.pagination.label" "Pagination"}}">
        {{if .HasPrev}}<a href="{{.PrevHref}}" class="page-link" aria-label="{{.T "admin.pagination.previous" "Previous page"}}">&larr;</a>{{end}}
        {{range .PageLinks}}{{if .Ellipsis}}<span class="page-ellipsis">&hellip;</span>{{else if .Current}}<span class="page-link page-link-current" aria-current="page">{{.Number}}</span>{{else}}<a href="{{.Href}}" class="page-link">{{.Number}}</a>{{end}}{{end}}
        {{if .HasNext}}<a href="{{.NextHref}}" class="page-link" aria-label="{{.T "admin.pagination.next" "Next page"}}">&rarr;</a>{{end}}
      </nav>
      {{end}}
    </div>
    {{else}}
    <div class="empty-state">
      <p class="empty-state-title">{{.T "admin.list.empty_title" "No %s rows yet" .ModelName}}</p>
      <p class="empty-state-body">{{if .SearchQuery}}{{.T "admin.list.empty_search" "Nothing matches “%s”." .SearchQuery}}{{else if .Criteria}}{{.T "admin.list.empty_filtered" "No rows match the selected filters."}}{{else}}{{.T "admin.list.empty_body" "Get started by adding the first one."}}{{end}}</p>
      {{if not .Criteria}}<a href="{{.BasePath}}new/" class="btn-primary mt-3">{{.T "admin.button.add_model" "+ Add %s" .ModelName}}</a>{{end}}
    </div>
    {{end}}
  </div>
  {{if .BulkDelete}}` + bulkScriptTag + `{{end}}
</div>
{{end}}`)

// bulkScriptTag loads the bulk delete script on a list page that enables it.
var bulkScriptTag = `<script src="` + bulkAsset.URL + `" defer></script>`

// widgetScriptTag loads the shared widget script on every form page, once.
// It serves the live character count of the Markdown widget, which the
// widget's own markup does not carry, and the preview and Format controls.
var widgetScriptTag = `<script src="` + widgetsAsset.URL + `" defer></script>`

var formTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <div>{{template "breadcrumbs" .}}<h1 class="topbar-title">{{.Title}}</h1></div>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-2xl">
    <div class="card-body">
      {{if .Error}}<div class="alert-error">{{.Error}}</div>{{end}}
      <form method="post">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      {{range .Fields}}<div class="field-group">{{.HTML}}{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}{{if .Error}}<p class="field-error" id="field-{{.Name}}-error" role="alert">{{.Error}}</p>{{end}}</div>
      {{end}}
        <div class="flex items-center gap-2 pt-2">
          <button type="submit" class="btn-primary">{{.T "admin.button.save" "Save"}}</button>
          <a href="../" class="btn-secondary">{{.T "admin.button.cancel" "Cancel"}}</a>
        </div>
      ` + widgetScriptTag + `
      </form>
    </div>
  </div>
</div>
{{end}}`)

var deleteTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <div>{{template "breadcrumbs" .}}<h1 class="topbar-title">{{.T "admin.delete.title" "Delete %s" .ModelName}}</h1></div>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      <p class="text-sm text-ink-muted mb-5">{{.T "admin.delete.confirm_prefix" "Are you sure you want to delete"}} <span class="font-medium text-ink">{{.ModelName}} {{.PK}}</span>? {{.T "admin.delete.confirm_suffix" "This action cannot be undone."}}</p>
      <form method="post" class="inline-row">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <button type="submit" class="btn-danger">{{.T "admin.button.confirm_delete" "Confirm delete"}}</button>
        <a href="../" class="btn-secondary">{{.T "admin.button.cancel" "Cancel"}}</a>
      </form>
    </div>
  </div>
</div>
{{end}}`)

type listRow struct {
	PK string
	// Identity names the row in the bulk delete confirmation: its first
	// ListDisplay value, or the key when that is blank.
	Identity string
	Cells    []listCell
}

type listPageData struct {
	chrome
	ModelName   string
	BasePath    string
	Columns     []string
	Rows        []listRow
	SearchQuery string
	Filters     []filterControl
	Criteria    bool
	Page        int
	TotalPages  int
	TotalCount  int
	PageLinks   []pageLink
	HasPrev     bool
	HasNext     bool
	PrevHref    string
	NextHref    string
	BulkDelete  bool
	ReturnTo    string
	// Deleted is the row count of a bulk delete the previous request
	// completed, or 0 when there is none to announce.
	Deleted int
}

type formField struct {
	Name     string
	HTML     template.HTML
	HelpText string
	Error    string
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
	CSRFToken string
}

var bulkRejectedTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <div>{{template "breadcrumbs" .}}<h1 class="topbar-title">{{.T "admin.bulk.rejected_title" "Nothing was deleted"}}</h1></div>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      <div class="alert-error" role="alert">{{.Message}}</div>
      <a href="{{.BasePath}}" class="btn-secondary">{{.T "admin.bulk.back" "Back to the list"}}</a>
    </div>
  </div>
</div>
{{end}}`)

type bulkRejectedPageData struct {
	chrome
	Message  string
	BasePath string
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

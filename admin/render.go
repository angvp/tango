package admin

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
)

const pageSize = 25

var listTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.Heading}}</h1>
  </div>
  <a href="{{.BasePath}}new/" class="btn-primary">{{.T "admin.button.add_model" "+ Add %s" .Singular}}</a>
</header>
<div class="content-body">
  <div class="card changelist-card">
    <div class="card-header">
      <form method="get" class="flex flex-wrap items-end gap-3 w-full">
        <div class="flex flex-col gap-1 flex-1 min-w-[200px]">
          <label for="list-search" class="field-caption">{{.T "admin.list.search_label" "Search"}}</label>
          <input id="list-search" type="search" name="q" value="{{.SearchQuery}}" placeholder="{{.T "admin.list.search_placeholder" "Search %s…" .Plural}}" class="input">
        </div>
        {{range .Filters}}<div class="flex flex-col gap-1">
          {{if .Options}}<label for="{{.ID}}" class="field-caption">{{.Label}}</label>
          <select id="{{.ID}}" name="{{.Key}}" class="input">{{range .Options}}<option value="{{.Value}}"{{if .Selected}} selected{{end}}>{{.Text}}</option>{{end}}</select>{{end}}
          {{if .AppliedValue}}<input type="hidden" name="{{.Key}}" value="{{.AppliedValue}}">{{end}}
          {{if .Message}}<p class="hint-small">{{.Message}}</p>{{end}}
        </div>{{end}}
        <button type="submit" class="btn-secondary shrink-0">{{.T "admin.button.search" "Search"}}</button>
        {{if .SearchQuery}}<a href="{{.ClearSearchHref}}" class="btn-ghost mb-1">{{.T "admin.list.clear_search" "Clear search"}}</a>{{end}}
      </form>
      {{if .AppliedFilters}}<p class="mt-3 text-sm text-ink-muted">{{.T "admin.list.filters_applied" "Filters applied:"}} {{range $i, $f := .AppliedFilters}}{{if $i}}, {{end}}{{$.T "admin.list.filter_pair" "%s: %s" $f.Label $f.Value}}{{end}} <a href="{{.ClearFiltersHref}}" class="btn-ghost">{{.T "admin.list.clear_filters" "Clear filters"}}</a></p>{{end}}
    </div>
    <form method="post" action="{{.DensityPath}}" class="density-bar">
      <input type="hidden" name="csrf_token" value="{{.SignedIn.CSRFToken}}">
      <input type="hidden" name="next" value="{{.ReturnTo}}">
      <span class="field-caption">{{.T "admin.density.label" "Row density"}}</span>
      <button type="submit" name="density" value="comfortable" aria-pressed="{{not .Compact}}" class="density-option {{if .Compact}}density-option-off{{else}}density-option-on{{end}}">{{.T "admin.density.comfortable" "Comfortable"}}</button>
      <button type="submit" name="density" value="compact" aria-pressed="{{.Compact}}" class="density-option {{if .Compact}}density-option-on{{else}}density-option-off{{end}}">{{.T "admin.density.compact" "Compact"}}</button>
    </form>
    {{if .Rows}}
    {{if .BulkDelete}}<div data-bulk-delete data-bulk-limit="10" data-msg-selected="{{.T "admin.bulk.selected" "{count} selected"}}" data-msg-none="{{.T "admin.bulk.select_first" "Select rows to delete them."}}" data-msg-title-one="{{.T "admin.bulk.title_one" "Delete 1 %s?" .Singular}}" data-msg-title-many="{{.T "admin.bulk.title_many" "Delete {count} %s?" .Plural}}" data-msg-confirm-one="{{.T "admin.bulk.confirm_one" "Delete 1 %s" .Singular}}" data-msg-confirm-many="{{.T "admin.bulk.confirm_many" "Delete {count} %s" .Plural}}" data-msg-more-one="{{.T "admin.bulk.more_one" "and 1 more"}}" data-msg-more-many="{{.T "admin.bulk.more_many" "and {count} more"}}">
    <div class="flex flex-wrap items-center gap-x-5 gap-y-2 border-b border-line-subtle px-5 py-3" data-bulk-bar hidden>
      <button type="button" class="btn-danger" data-bulk-open aria-describedby="bulk-count">{{.T "admin.bulk.delete_selected" "Delete selected"}}</button>
      <span id="bulk-count" class="text-sm text-ink-muted" data-bulk-count aria-live="polite">{{.T "admin.bulk.select_first" "Select rows to delete them."}}</span>
      <label class="inline-flex min-h-6 min-w-6 items-center gap-2 text-sm text-ink md:hidden"><input type="checkbox" data-bulk-select-all class="size-4">{{.T "admin.bulk.select_page" "Select all on this page"}}</label>
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
          <button type="submit" class="btn-danger" data-bulk-confirm>{{.T "admin.bulk.confirm_many" "Delete {count} %s" .Plural}}</button>
          <button type="button" class="btn-secondary" data-bulk-cancel autofocus>{{.T "admin.button.cancel" "Cancel"}}</button>
        </div>
      </form>
    </dialog>{{end}}
    <div class="table-wrap">
      <table class="table-admin {{if .Compact}}table-compact{{else}}table-comfortable{{end}}">
        <caption class="sr-only">{{.Heading}}</caption>
        <thead><tr>{{if .BulkDelete}}<th scope="col" data-bulk-cell hidden><input type="checkbox" data-bulk-select-all class="max-md:hidden" aria-label="{{.T "admin.bulk.select_all" "Select all %d rows on this page" (len .Rows)}}"></th>{{end}}{{range .Columns}}<th scope="col"{{if .Numeric}} class="cell-num"{{end}}>{{.Label}}</th>{{end}}<th scope="col" class="cell-actions">{{.T "admin.list.actions" "Actions"}}</th></tr></thead>
        <tbody>
        {{range .Rows}}<tr>
          {{if $.BulkDelete}}<td data-bulk-cell hidden><input type="checkbox" data-bulk-pk="{{.PK}}" data-bulk-identity="{{.Identity}}" aria-label="{{$.T "admin.bulk.select_row" "Select %s" .Identity}}"></td>{{end}}
          {{range .Cells}}<td{{if .Numeric}} class="cell-num"{{end}}><span class="cell-label">{{.Label}}</span>{{if .Href}}<a href="{{.Href}}" rel="noopener noreferrer">{{.Text}}</a>{{else if eq .Badge "yes"}}<span class="badge-true">{{$.T "admin.boolean.yes" "Yes"}}</span>{{else if eq .Badge "no"}}<span class="badge-false">{{$.T "admin.boolean.no" "No"}}</span>{{else}}{{.Text}}{{end}}</td>{{end}}
          <td class="cell-actions">
            <div class="table-actions">
              <a href="{{$.BasePath}}{{.PK}}/" class="btn-ghost" aria-label="{{$.T "admin.list.edit_row" "Edit %s" .Identity}}">{{$.T "admin.button.edit" "Edit"}}</a>
              <a href="{{$.BasePath}}{{.PK}}/delete/" class="btn-ghost text-danger-fg hover:bg-danger-bg" aria-label="{{$.T "admin.list.delete_row" "Delete %s" .Identity}}">{{$.T "admin.button.delete" "Delete"}}</a>
            </div>
          </td>
        </tr>{{end}}
        </tbody>
      </table>
    </div>
    {{if .BulkDelete}}</div>{{end}}
    <div class="pagination">
      <span>{{if gt .TotalPages 1}}{{.T "admin.list.showing" "Showing %d to %d of %d %s" .ResultFirst .ResultLast .TotalCount .Plural}}{{else if eq .TotalCount 1}}{{.T "admin.list.count_one" "%d %s" .TotalCount .Singular}}{{else}}{{.T "admin.list.count_many" "%d %s" .TotalCount .Plural}}{{end}}</span>
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
      <p class="empty-state-title">{{.T "admin.list.empty_title" "No %s yet" .Plural}}</p>
      <p class="empty-state-body">{{if .SearchQuery}}{{.T "admin.list.empty_no_match" "No %s match “%s”." .Plural .SearchQuery}}{{else if .Criteria}}{{.T "admin.list.empty_no_filter_match" "No %s match the selected filters." .Plural}}{{else}}{{.T "admin.list.empty_first" "Add the first %s to see it here." .Singular}}{{end}}</p>
      {{if not .Criteria}}<a href="{{.BasePath}}new/" class="btn-primary mt-3">{{.T "admin.button.add_model" "+ Add %s" .Singular}}</a>{{end}}
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
    <div>{{template "breadcrumbs" .}}<h1 class="topbar-title">{{.T "admin.delete.title" "Delete this %s" .Singular}}</h1></div>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      {{if .Error}}<div class="alert-error" role="alert">{{.Error}}</div>
      <a href="../" class="btn-secondary">{{.T "admin.button.cancel" "Cancel"}}</a>
      {{else}}<p class="text-sm text-ink-muted mb-5">{{.T "admin.delete.confirm_prefix" "Are you sure you want to delete"}} <span class="font-medium text-ink">{{.Label}}</span>? {{.T "admin.delete.confirm_suffix" "This action cannot be undone."}}</p>
      <form method="post" class="inline-row">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <button type="submit" class="btn-danger">{{.T "admin.delete.confirm_button" "Delete %s" .Singular}}</button>
        <a href="../" class="btn-secondary">{{.T "admin.button.cancel" "Cancel"}}</a>
      </form>{{end}}
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

// listColumn is one changelist header. Numeric columns are right-aligned.
type listColumn struct {
	Label   string
	Numeric bool
}

type listPageData struct {
	chrome
	// Heading is the model's plural noun, capitalized, for the page title.
	Heading string
	// Singular and Plural are the model's display nouns (admin.Options).
	Singular    string
	Plural      string
	BasePath    string
	Columns     []listColumn
	Compact     bool
	DensityPath string
	Rows        []listRow
	SearchQuery string
	// ClearSearchHref keeps the applied filters and drops the search.
	ClearSearchHref string
	Filters         []filterControl
	// AppliedFilters names each applied filter in the summary above the table.
	AppliedFilters []filterSummary
	// ClearFiltersHref keeps the search and drops the applied filters.
	ClearFiltersHref string
	// ResultFirst and ResultLast are the position of the page's first and last
	// rows among all the matching rows.
	ResultFirst int
	ResultLast  int
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
	Singular string
	// Label names the row: its display label, or the noun and primary key.
	Label string
	// Error is the refusal shown instead of the confirm form, when the database
	// will not delete the row because other rows still reference it.
	Error     string
	CSRFToken string
}

var bulkRejectedTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <div>{{template "breadcrumbs" .}}<h1 class="topbar-title">{{.Title}}</h1></div>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      <div class="alert-error" role="alert">{{.Message}}</div>
      <a href="{{.BasePath}}" class="btn-secondary">{{.T "admin.bulk.back_to" "Back to %s" .Plural}}</a>
    </div>
  </div>
</div>
{{end}}`)

type bulkRejectedPageData struct {
	chrome
	Title    string
	Message  string
	Plural   string
	BasePath string
}

func render(ctx *tango.Context, status int, tmpl *template.Template, data any) error {
	// A successful GET that shows the signed-in layout has displayed any outcome
	// the request carried, so the cookie is expired by that response. Any other
	// response leaves it for the next page.
	if status/100 == 2 && ctx.Request().Method == http.MethodGet && signedInFrom(ctx.Context()) != nil && hasFlashCookie(ctx.Request()) {
		clearFlashCookie(ctx.ResponseWriter(), ctx.Request())
	}
	return ctx.HTML(status, tmpl, "layout", data)
}

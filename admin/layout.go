package admin

import (
	"context"
	"html/template"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/i18nbridge"
)

// tailwindScriptTag loads Tailwind's Play CDN: no Node/npm build step,
// JIT-compiled utility classes at runtime.
const tailwindScriptTag = `<script src="https://cdn.tailwindcss.com"></script>`

// navItem is one entry in the admin's sidebar navigation: every registered
// model, so any admin page can link to any other without a page reload.
type navItem struct {
	Name string
	Path string
}

// chrome is the data every admin page shares: the sidebar's contents, which
// entry is current, and the admin's branding. Page-specific data structs
// embed it.
type chrome struct {
	Nav    []navItem
	Active string
	Brand  Branding
	ctx    context.Context
}

func (c chrome) withContext(ctx context.Context) chrome {
	c.ctx = ctx
	return c
}

// SignedIn is the account shown in the sidebar footer, or nil when the page
// was not served inside a session (the layout rendered on its own).
func (c chrome) SignedIn() *signedIn {
	if c.ctx == nil {
		return nil
	}
	return signedInFrom(c.ctx)
}

// Lang is the registered locale of the catalog the request selected, or "en"
// when none was, for the page's lang attribute.
func (c chrome) Lang() string {
	return pageLang(c.ctx)
}

func (c chrome) T(key, fallback string, args ...any) string {
	return i18n.T(c.ctx, key, fallback, args...)
}

// layoutTemplate is the shared base layout every admin page renders into.
// Pages supply their body by defining a "content" template in the same
// *template.Template set (see cloneWithContent); executed on its own, the
// "content" block falls back to empty output so the layout stays valid,
// renderable HTML in isolation.
var layoutTemplate = template.Must(template.New("layout").Parse(`<!doctype html>
<html lang="{{.Lang}}"{{with .Theme}} data-theme="{{.}}"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.T "admin.layout.title" "tanGO Admin"}}</title>
` + tailwindScriptTag + `
<script>` + tailwindThemeConfig() + `</script>
<style>` + themeCSS() + `</style>
<style type="text/tailwindcss">
@layer base {
  html, body { @apply overflow-x-hidden; }
  ::selection { @apply bg-selection-bg text-selection-fg; }
  ::-webkit-scrollbar { @apply w-2.5 h-2.5; }
  ::-webkit-scrollbar-track { @apply bg-transparent; }
  ::-webkit-scrollbar-thumb { @apply bg-line-strong rounded-full; }
  ::-webkit-scrollbar-thumb:hover { @apply bg-control-line; }
  :focus-visible { @apply outline-none ring-2 ring-ring ring-offset-2 ring-offset-surface; }
}
@layer components {
  .shell { @apply min-h-screen flex bg-surface-sunken text-ink antialiased; }

  .sidebar { @apply w-64 shrink-0 bg-sidebar text-sidebar-fg border-r border-sidebar-strong/10 flex flex-col fixed inset-y-0 left-0 z-40 -translate-x-full transition-transform duration-200 ease-out md:relative md:translate-x-0; }
  .sidebar-open { @apply translate-x-0; }
  .sidebar-backdrop { @apply fixed inset-0 z-30 bg-sidebar/50 hidden md:hidden; }
  .sidebar-backdrop-open { @apply block; }
  .menu-btn { @apply md:hidden inline-flex items-center justify-center h-9 w-9 rounded-md text-ink-faint hover:bg-surface-hover hover:text-ink -ml-1.5; }
  .sidebar-brand { @apply flex items-center gap-2.5 px-5 h-16 border-b border-sidebar-strong/10 shrink-0; }
  .sidebar-brand-mark { @apply flex h-7 w-7 items-center justify-center rounded-md bg-sidebar-accent text-sidebar-accent-fg text-sm font-bold; }
  .sidebar-brand-text { @apply text-sidebar-strong font-semibold tracking-tight text-[15px]; }
  .sidebar-section-label { @apply px-3 pt-5 pb-2 text-[11px] font-semibold uppercase tracking-wider text-sidebar-muted; }
  .sidebar-nav { @apply flex-1 overflow-y-auto px-3 pb-4; }
  .sidebar-link { @apply flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium text-sidebar-fg hover:bg-sidebar-strong/5 hover:text-sidebar-strong transition-colors; }
  .sidebar-link-active { @apply bg-sidebar-accent/15 text-sidebar-strong; }
  .sidebar-link-dot { @apply h-1.5 w-1.5 shrink-0 rounded-full bg-sidebar-dot group-hover:bg-sidebar-muted; }
  .sidebar-link-dot-active { @apply bg-sidebar-accent; }
  .sidebar-footer { @apply px-5 py-4 border-t border-sidebar-strong/10 text-xs text-sidebar-muted shrink-0; }
  .sidebar-logout { @apply mt-2; }
  .sidebar-logout-button { @apply text-sidebar-muted underline hover:text-sidebar-strong cursor-pointer; }

  .topbar { @apply sticky top-0 z-10 flex flex-wrap items-center justify-between gap-3 border-b border-line bg-surface/85 backdrop-blur px-4 py-3 sm:h-16 sm:px-6 sm:py-0 min-w-0; }
  .topbar-title { @apply text-lg font-semibold tracking-tight text-ink; }
  .topbar-meta { @apply text-sm text-ink-faint hidden sm:block; }

  .content { @apply flex-1 min-w-0 flex flex-col; }
  .content-body { @apply flex-1 min-w-0 px-4 py-5 sm:px-6 sm:py-6 max-w-5xl w-full mx-auto; }

  .card { @apply bg-surface border border-line rounded-xl shadow-sm; }
  .card-header { @apply flex flex-wrap items-center justify-between gap-3 border-b border-line-subtle px-5 py-4; }
  .card-body { @apply p-5; }

  .btn { @apply inline-flex items-center justify-center gap-1.5 rounded-md px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none; }
  .btn-primary { @apply btn bg-accent text-accent-fg shadow-sm hover:bg-accent-hover; }
  .btn-secondary { @apply btn bg-surface text-ink-muted border border-control-line shadow-sm hover:bg-surface-sunken; }
  .btn-danger { @apply btn bg-danger-solid text-danger-solid-fg shadow-sm hover:bg-danger-solid-hover; }
  .btn-ghost { @apply btn text-ink-muted hover:bg-surface-hover hover:text-ink px-2.5 py-1.5; }

  .input { @apply block w-full rounded-md border border-control-line bg-surface px-3 py-2 text-sm text-ink placeholder:text-ink-faint shadow-sm focus:border-ring; }
  .json-input { @apply font-mono text-xs; }
  .widget-tabs { @apply flex gap-1 border-b border-line-strong mb-2; }
  .widget-tab { @apply px-3 py-1.5 text-sm font-medium text-ink-muted border border-transparent rounded-t-md -mb-px hover:text-ink focus:outline-none focus-visible:ring-2 focus-visible:ring-ring; }
  .widget-tab[aria-selected="true"] { @apply bg-surface text-ink border-line-strong border-b-surface; }
  .widget-tabs[hidden], .widget-panel[hidden], [data-json-apply][hidden] { display: none; }
  .json-formatted { @apply block w-full overflow-auto rounded-md border border-control-line bg-surface-sunken px-3 py-2 font-mono text-xs text-ink whitespace-pre; min-height: 10rem; }
  .json-formatted[data-invalid] { @apply text-danger-fg whitespace-pre-wrap; }
  .field-label { @apply block text-sm font-medium text-ink-muted mb-1.5; }
  .field-group { @apply mb-4 last:mb-0; }
  .checkbox-row { @apply flex items-center gap-2.5; }
  .checkbox { @apply h-4 w-4 rounded border-control-line text-accent focus:ring-ring; }

  .table-wrap { @apply overflow-x-auto; }
  .table-admin { @apply w-full text-sm; }
  .table-admin thead th { @apply text-left text-xs font-semibold uppercase tracking-wider text-ink-faint bg-surface-sunken px-5 py-3 border-b border-line; }
  .table-admin tbody td { @apply px-5 py-3.5 border-b border-line-subtle text-ink-muted; }
  .table-admin tbody tr { @apply hover:bg-surface-sunken/80 transition-colors; }
  .table-admin tbody tr:last-child td { @apply border-b-0; }
  .table-actions { @apply flex items-center justify-end gap-1; }

  .badge { @apply inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium; }
  .badge-true { @apply badge bg-success-bg text-success-fg; }
  .badge-false { @apply badge bg-neutral-bg text-neutral-fg; }

  .pagination { @apply flex flex-wrap items-center justify-between gap-3 border-t border-line-subtle px-5 py-3.5 text-sm text-ink-faint; }
  .pagination-pages { @apply flex items-center gap-1; }
  .page-link { @apply inline-flex items-center justify-center min-w-[2rem] h-8 px-2 rounded-md text-sm font-medium text-ink-muted hover:bg-surface-hover hover:text-ink transition-colors; }
  .page-link-current { @apply bg-accent text-accent-fg hover:bg-accent hover:text-accent-fg; }
  .page-ellipsis { @apply inline-flex items-center justify-center h-8 px-1 text-sm text-ink-faint; }

  .alert-error { @apply mb-5 rounded-md border border-danger-border bg-danger-bg px-4 py-3 text-sm text-danger-fg; }

  .field-help { @apply mt-1 text-sm text-ink-muted; }
  .field-error { @apply mt-1 text-sm text-danger-fg; }

  .empty-state { @apply flex flex-col items-center justify-center gap-1 px-5 py-16 text-center; }
  .empty-state-title { @apply text-sm font-medium text-ink-muted; }
  .empty-state-body { @apply text-sm text-ink-faint; }
}
</style>

{{define "menuBtn"}}<button type="button" class="menu-btn" onclick="tangoAdminNav.open()" aria-label="{{.T "admin.layout.open_menu" "Open menu"}}">
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="h-5 w-5"><path d="M4 6h16M4 12h16M4 18h16"/></svg>
</button>{{end}}
</head>
<body>
<div class="shell">
<div id="sidebar-backdrop" class="sidebar-backdrop" onclick="tangoAdminNav.close()"></div>
<aside id="sidebar" class="sidebar">
  <div class="sidebar-brand">
    {{if .Brand.LogoURL}}<img src="{{.Brand.LogoURL}}" alt="" class="h-7 w-7 rounded-md object-cover">{{else}}<span class="sidebar-brand-mark">t</span>{{end}}
    <span class="sidebar-brand-text">{{if .Brand.Name}}{{.Brand.Name}}{{else}}{{.T "admin.layout.brand" "tanGO Admin"}}{{end}}</span>
  </div>
  <div class="sidebar-nav">
    <div class="sidebar-section-label">{{.T "admin.layout.models" "Models"}}</div>
    {{$active := .Active}}
    {{range .Nav}}
    <a href="{{.Path}}" class="sidebar-link group{{if eq .Name $active}} sidebar-link-active{{end}}">
      <span class="sidebar-link-dot{{if eq .Name $active}} sidebar-link-dot-active{{end}}"></span>
      {{.Name}}
    </a>
    {{end}}
  </div>
  <div class="sidebar-footer">{{with .SignedIn}}
    <div>{{$.T "admin.layout.signed_in_as" "Signed in as %s" .Username}}</div>
    <form method="post" action="/admin/theme/" role="group" aria-label="{{$.T "admin.theme.label" "Theme"}}" class="mt-2 flex flex-wrap gap-1">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <input type="hidden" name="next" value="{{$.ThemeNext}}">
      <button type="submit" name="theme" value="light" aria-pressed="{{if eq $.Theme "light"}}true{{else}}false{{end}}" class="rounded-md px-2 py-1 text-xs font-medium {{if eq $.Theme "light"}}bg-sidebar-accent text-sidebar-accent-fg{{else}}text-sidebar-muted hover:text-sidebar-strong{{end}}">{{$.T "admin.theme.light" "Light"}}</button>
      <button type="submit" name="theme" value="dark" aria-pressed="{{if eq $.Theme "dark"}}true{{else}}false{{end}}" class="rounded-md px-2 py-1 text-xs font-medium {{if eq $.Theme "dark"}}bg-sidebar-accent text-sidebar-accent-fg{{else}}text-sidebar-muted hover:text-sidebar-strong{{end}}">{{$.T "admin.theme.dark" "Dark"}}</button>
      <button type="submit" name="theme" value="system" aria-pressed="{{if eq $.Theme ""}}true{{else}}false{{end}}" class="rounded-md px-2 py-1 text-xs font-medium {{if eq $.Theme ""}}bg-sidebar-accent text-sidebar-accent-fg{{else}}text-sidebar-muted hover:text-sidebar-strong{{end}}">{{$.T "admin.theme.system" "System"}}</button>
    </form>
    <form method="post" action="/admin/logout/" class="sidebar-logout">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <button type="submit" class="sidebar-logout-button">{{$.T "admin.layout.logout" "Log out"}}</button>
    </form>{{end}}
  </div>
</aside>
<div class="content">
{{block "content" .}}{{end}}
</div>
</div>
<script>
  window.tangoAdminNav = {
    open: function () {
      document.getElementById("sidebar").classList.add("sidebar-open");
      document.getElementById("sidebar-backdrop").classList.add("sidebar-backdrop-open");
    },
    close: function () {
      document.getElementById("sidebar").classList.remove("sidebar-open");
      document.getElementById("sidebar-backdrop").classList.remove("sidebar-backdrop-open");
    }
  };
</script>
</body>
</html>
`))

// freshLayout returns a fresh, never-executed clone of layoutTemplate.
// html/template forbids cloning a template after it has executed, so
// layoutTemplate itself is never executed directly — only clones are.
func freshLayout() *template.Template {
	return template.Must(layoutTemplate.Clone())
}

// cloneWithContent returns a fresh layout clone with "content" defined from
// contentSource, so ExecuteTemplate(w, "layout", data) renders the shared
// head/nav/Tailwind setup around that page's body.
func cloneWithContent(contentSource string) *template.Template {
	return template.Must(freshLayout().New("content").Parse(contentSource))
}

// pageLang is the value of an admin page's lang attribute: the registered
// locale a catalog was selected for, else "en", the language of the built-in
// text.
func pageLang(ctx context.Context) string {
	if ctx == nil || i18nbridge.Selected == nil {
		return "en"
	}
	if lang := i18nbridge.Selected(ctx); lang != "" {
		return lang
	}
	return "en"
}

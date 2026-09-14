package admin

import (
	"context"
	"html/template"

	"github.com/angvp/tango/i18n"
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

func (c chrome) T(key, fallback string, args ...any) string {
	return i18n.T(c.ctx, key, fallback, args...)
}

// layoutTemplate is the shared base layout every admin page renders into.
// Pages supply their body by defining a "content" template in the same
// *template.Template set (see cloneWithContent); executed on its own, the
// "content" block falls back to empty output so the layout stays valid,
// renderable HTML in isolation.
var layoutTemplate = template.Must(template.New("layout").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.T "admin.layout.title" "tanGO Admin"}}</title>
` + tailwindScriptTag + `
<style type="text/tailwindcss">
@layer base {
  html, body { @apply overflow-x-hidden; }
  ::selection { @apply bg-indigo-200 text-indigo-950; }
  ::-webkit-scrollbar { @apply w-2.5 h-2.5; }
  ::-webkit-scrollbar-track { @apply bg-transparent; }
  ::-webkit-scrollbar-thumb { @apply bg-slate-300 rounded-full; }
  ::-webkit-scrollbar-thumb:hover { @apply bg-slate-400; }
  :focus-visible { @apply outline-none ring-2 ring-indigo-500 ring-offset-2 ring-offset-white; }
}
@layer components {
  .shell { @apply min-h-screen flex bg-slate-50 text-slate-900 antialiased; }

  .sidebar { @apply w-64 shrink-0 bg-slate-950 text-slate-300 flex flex-col fixed inset-y-0 left-0 z-40 -translate-x-full transition-transform duration-200 ease-out md:relative md:translate-x-0; }
  .sidebar-open { @apply translate-x-0; }
  .sidebar-backdrop { @apply fixed inset-0 z-30 bg-slate-950/50 hidden md:hidden; }
  .sidebar-backdrop-open { @apply block; }
  .menu-btn { @apply md:hidden inline-flex items-center justify-center h-9 w-9 rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-900 -ml-1.5; }
  .sidebar-brand { @apply flex items-center gap-2.5 px-5 h-16 border-b border-white/10 shrink-0; }
  .sidebar-brand-mark { @apply flex h-7 w-7 items-center justify-center rounded-md bg-indigo-500 text-white text-sm font-bold; }
  .sidebar-brand-text { @apply text-white font-semibold tracking-tight text-[15px]; }
  .sidebar-section-label { @apply px-3 pt-5 pb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500; }
  .sidebar-nav { @apply flex-1 overflow-y-auto px-3 pb-4; }
  .sidebar-link { @apply flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium text-slate-300 hover:bg-white/5 hover:text-white transition-colors; }
  .sidebar-link-active { @apply bg-indigo-500/15 text-white; }
  .sidebar-link-dot { @apply h-1.5 w-1.5 shrink-0 rounded-full bg-slate-600 group-hover:bg-slate-400; }
  .sidebar-link-dot-active { @apply bg-indigo-400; }
  .sidebar-footer { @apply px-5 py-4 border-t border-white/10 text-xs text-slate-500 shrink-0; }

  .topbar { @apply sticky top-0 z-10 flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 bg-white/85 backdrop-blur px-4 py-3 sm:h-16 sm:px-6 sm:py-0 min-w-0; }
  .topbar-title { @apply text-lg font-semibold tracking-tight text-slate-900; }
  .topbar-meta { @apply text-sm text-slate-500 hidden sm:block; }

  .content { @apply flex-1 min-w-0 flex flex-col; }
  .content-body { @apply flex-1 min-w-0 px-4 py-5 sm:px-6 sm:py-6 max-w-5xl w-full mx-auto; }

  .card { @apply bg-white border border-slate-200 rounded-xl shadow-sm; }
  .card-header { @apply flex flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-5 py-4; }
  .card-body { @apply p-5; }

  .btn { @apply inline-flex items-center justify-center gap-1.5 rounded-md px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none; }
  .btn-primary { @apply btn bg-indigo-600 text-white shadow-sm hover:bg-indigo-500; }
  .btn-secondary { @apply btn bg-white text-slate-700 border border-slate-300 shadow-sm hover:bg-slate-50; }
  .btn-danger { @apply btn bg-red-600 text-white shadow-sm hover:bg-red-500; }
  .btn-ghost { @apply btn text-slate-600 hover:bg-slate-100 hover:text-slate-900 px-2.5 py-1.5; }

  .input { @apply block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 shadow-sm focus:border-indigo-500; }
  .field-label { @apply block text-sm font-medium text-slate-700 mb-1.5; }
  .field-group { @apply mb-4 last:mb-0; }
  .checkbox-row { @apply flex items-center gap-2.5; }
  .checkbox { @apply h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500; }

  .table-wrap { @apply overflow-x-auto; }
  .table-admin { @apply w-full text-sm; }
  .table-admin thead th { @apply text-left text-xs font-semibold uppercase tracking-wider text-slate-500 bg-slate-50 px-5 py-3 border-b border-slate-200; }
  .table-admin tbody td { @apply px-5 py-3.5 border-b border-slate-100 text-slate-700; }
  .table-admin tbody tr { @apply hover:bg-slate-50/80 transition-colors; }
  .table-admin tbody tr:last-child td { @apply border-b-0; }
  .table-actions { @apply flex items-center justify-end gap-1; }

  .badge { @apply inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium; }
  .badge-true { @apply badge bg-emerald-100 text-emerald-700; }
  .badge-false { @apply badge bg-slate-100 text-slate-500; }

  .pagination { @apply flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 px-5 py-3.5 text-sm text-slate-500; }
  .pagination-pages { @apply flex items-center gap-1; }
  .page-link { @apply inline-flex items-center justify-center min-w-[2rem] h-8 px-2 rounded-md text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900 transition-colors; }
  .page-link-current { @apply bg-indigo-600 text-white hover:bg-indigo-600 hover:text-white; }
  .page-ellipsis { @apply inline-flex items-center justify-center h-8 px-1 text-sm text-slate-400; }

  .alert-error { @apply mb-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700; }

  .empty-state { @apply flex flex-col items-center justify-center gap-1 px-5 py-16 text-center; }
  .empty-state-title { @apply text-sm font-medium text-slate-700; }
  .empty-state-body { @apply text-sm text-slate-500; }
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
  <div class="sidebar-footer">{{.T "admin.layout.signed_in" "Signed in over Basic Auth"}}</div>
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

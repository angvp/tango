package admin

import (
	"context"
	"html/template"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/i18nbridge"
)

// navScriptTag loads the sidebar script on every admin page.
var navScriptTag = `<script src="` + navAsset.URL + `" defer></script>`

// stylesheetLinkTag loads the precompiled admin stylesheet every admin page
// shares.
var stylesheetLinkTag = `<link rel="stylesheet" href="` + adminStyleAsset.URL + `">`

// navItem is one entry in the admin's sidebar navigation: every registered
// model, so any admin page can link to any other without a page reload.
type navItem struct {
	Name string
	// Label is the sidebar text: the model's plural display noun.
	Label string
	Path  string
}

// chrome is the data every admin page shares: the sidebar's contents, which
// entry is current, and the admin's branding. Page-specific data structs
// embed it.
type chrome struct {
	Nav    []navItem
	Active string
	// PageTitle names the page in the document title, ahead of the admin's
	// name. A page without one has the admin's name alone.
	PageTitle string
	Brand     Branding
	// Crumbs is the trail above the page title; a page without one renders
	// no breadcrumb navigation.
	Crumbs []crumb
	ctx    context.Context
}

// withPageTitle returns the chrome with the document title's page name set.
func (c chrome) withPageTitle(title string) chrome {
	c.PageTitle = title
	return c
}

// crumb is one step of the breadcrumb trail. The current page has no Href
// and renders as text.
type crumb struct {
	Label string
	Href  string
}

// withCrumbs returns the chrome with the breadcrumb trail set.
func (c chrome) withCrumbs(crumbs ...crumb) chrome {
	c.Crumbs = crumbs
	return c
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

// Notice is the outcome the status region shows, or nil when the page has none
// or was not served inside a session.
func (c chrome) Notice() *statusNotice {
	if c.ctx == nil {
		return nil
	}
	if who := signedInFrom(c.ctx); who != nil {
		return who.Notice
	}
	return nil
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
<title>{{with .PageTitle}}{{.}} · {{end}}{{.T "admin.layout.title" "tanGO Admin"}}</title>
` + stylesheetLinkTag + `

{{define "menuBtn"}}<button type="button" class="menu-btn" data-admin-nav-open aria-expanded="false" aria-controls="sidebar" aria-label="{{.T "admin.layout.open_menu" "Open menu"}}">
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="h-5 w-5"><path d="M4 6h16M4 12h16M4 18h16"/></svg>
</button>{{end}}

{{define "breadcrumbs"}}{{with .Crumbs}}<nav class="breadcrumb" aria-label="{{$.T "admin.layout.breadcrumb" "Breadcrumb"}}"><ol class="breadcrumb-list">{{range .}}
  <li class="breadcrumb-item">{{if .Href}}<a href="{{.Href}}" class="breadcrumb-link">{{.Label}}</a>{{else}}<span class="breadcrumb-current" aria-current="page">{{.Label}}</span>{{end}}</li>{{end}}
</ol></nav>{{end}}{{end}}
</head>
<body>
<a href="#main" class="skip-link">{{.T "admin.layout.skip" "Skip to content"}}</a>
<div class="shell">
<div id="sidebar-backdrop" class="sidebar-backdrop" data-admin-nav-close></div>
<aside id="sidebar" class="sidebar">
  <button type="button" class="drawer-close" data-admin-nav-close>
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="h-4 w-4" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg>
    {{.T "admin.layout.close_menu" "Close menu"}}
  </button>
  <div class="sidebar-brand">
    {{if .Brand.LogoURL}}<img src="{{.Brand.LogoURL}}" alt="" class="h-7 w-7 rounded-md object-cover">{{else}}<span class="sidebar-brand-mark">t</span>{{end}}
    <span class="sidebar-brand-text">{{if .Brand.Name}}{{.Brand.Name}}{{else}}{{.T "admin.layout.brand" "tanGO Admin"}}{{end}}</span>
  </div>
  <nav class="sidebar-nav" aria-label="{{.T "admin.layout.models" "Models"}}">
    <div class="sidebar-section-label">{{.T "admin.layout.models" "Models"}}</div>
    {{$active := .Active}}
    {{range .Nav}}
    <a href="{{.Path}}" class="sidebar-link group{{if eq .Name $active}} sidebar-link-active{{end}}"{{if eq .Name $active}} aria-current="page"{{end}}>
      <span class="sidebar-link-dot{{if eq .Name $active}} sidebar-link-dot-active{{end}}"></span>
      {{.Label}}
    </a>
    {{end}}
  </nav>
  <div class="sidebar-footer">{{with .SignedIn}}
    <div>{{$.T "admin.layout.signed_in_as" "Signed in as %s" .Username}}</div>
    <form method="post" action="/admin/theme/" role="group" aria-label="{{$.T "admin.theme.label" "Theme"}}" class="mt-2 flex flex-wrap gap-1">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <input type="hidden" name="next" value="{{$.ThemeNext}}">
      {{$on := eq $.Theme "light"}}<button type="submit" name="theme" value="light" aria-pressed="{{$on}}" class="theme-option {{if $on}}theme-option-on{{else}}theme-option-off{{end}}">{{$.T "admin.theme.light" "Light"}}</button>
      {{$on := eq $.Theme "dark"}}<button type="submit" name="theme" value="dark" aria-pressed="{{$on}}" class="theme-option {{if $on}}theme-option-on{{else}}theme-option-off{{end}}">{{$.T "admin.theme.dark" "Dark"}}</button>
      {{$on := eq $.Theme ""}}<button type="submit" name="theme" value="system" aria-pressed="{{$on}}" class="theme-option {{if $on}}theme-option-on{{else}}theme-option-off{{end}}">{{$.T "admin.theme.system" "System"}}</button>
    </form>
    <form method="post" action="/admin/logout/" class="sidebar-logout">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <button type="submit" class="sidebar-logout-button">{{$.T "admin.layout.logout" "Log out"}}</button>
    </form>{{end}}
  </div>
</aside>
<main id="main" tabindex="-1" class="content">
<div id="admin-status" class="status-region" role="status">{{with .Notice}}<div class="alert-success status-message"><p class="status-text">{{.Text}}</p><form method="post" action="/admin/messages/dismiss/" class="status-dismiss"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><input type="hidden" name="next" value="{{.Next}}"><button type="submit" class="btn-secondary">{{.Dismiss}}</button></form></div>{{end}}</div>
{{block "content" .}}{{end}}
</main>
</div>
` + navScriptTag + `
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

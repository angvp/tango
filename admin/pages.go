package admin

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
)

// Page is a staff-only page a host adds to the admin. It is served at
// /admin/<Path>/ for GET and POST, behind the same session gate as the admin's
// own pages, inside the admin's route group.
type Page struct {
	// Path is one URL segment: lowercase letters, digits, "-" and "_", starting
	// with a letter. It must not name a model's table or an admin route.
	Path string
	// Title is the sidebar text when Nav is set.
	Title string
	// View answers the request. It runs only for a signed-in staff account.
	View tango.View
	// Nav lists the page in the sidebar, after the models.
	Nav bool
}

var (
	// ErrInvalidPage is returned for a page with a bad path, no title or no view.
	ErrInvalidPage = errors.New("admin: invalid page")
	// ErrReservedPagePath is returned for a path the admin uses for itself.
	ErrReservedPagePath = errors.New("admin: page path is reserved")
	// ErrDuplicatePage is returned when two pages share a path.
	ErrDuplicatePage = adminregistry.ErrDuplicatePage
)

var pagePathRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// reservedPagePaths are the first segments the admin serves itself.
var reservedPagePaths = map[string]struct{}{
	"login": {}, "logout": {}, "theme": {}, "messages": {}, "density": {},
}

// RegisterPage adds a staff-only page to the admin. Call it before the admin
// app is installed, as for a model: a page registered after admin.New runs is
// reported by tango check and is not served.
func RegisterPage(registry *tango.Registry, page Page) error {
	if registry == nil {
		return fmt.Errorf("%w: nil registry", ErrInvalidPage)
	}
	if !pagePathRE.MatchString(page.Path) {
		return fmt.Errorf("%w: path %q must match %s", ErrInvalidPage, page.Path, pagePathRE)
	}
	if _, reserved := reservedPagePaths[page.Path]; reserved {
		return fmt.Errorf("%w: %q", ErrReservedPagePath, page.Path)
	}
	if page.View == nil {
		return fmt.Errorf("%w: %q has no View", ErrInvalidPage, page.Path)
	}
	if page.Nav && page.Title == "" {
		return fmt.Errorf("%w: %q is listed in the sidebar but has no Title", ErrInvalidPage, page.Path)
	}
	return registry.Admin().AddPage(adminregistry.PageEntry{
		Path: page.Path, Title: page.Title, Nav: page.Nav, View: page.View,
	})
}

// pagePath is the canonical URL of a page.
func pagePath(path string) string { return "/admin/" + path + "/" }

// mountedPage is a registered page whose View has been checked.
type mountedPage struct {
	entry adminregistry.PageEntry
	view  tango.View
}

// pageNavKey is the sidebar key of a page, which the nav entry and the current
// page's Active marker share.
func pageNavKey(path string) string { return "page:" + path }

// checkedPages returns the pages to mount. It refuses a page whose View is not
// a tango.View (the registry holds it as an any) and a page whose path is also
// a registered model's admin path, which would answer the same URL.
func checkedPages(entries []adminregistry.PageEntry, registrations []ModelRegistration) ([]mountedPage, error) {
	models := make(map[string]string, len(registrations))
	for _, registration := range registrations {
		models[modelAdminPath(registration.Model.Name)] = registration.Model.Name
	}
	mounted := make([]mountedPage, 0, len(entries))
	for _, entry := range entries {
		if name, clash := models[pagePath(entry.Path)]; clash {
			return nil, fmt.Errorf("%w: %q is also the admin path of model %s", ErrInvalidPage, entry.Path, name)
		}
		view, ok := entry.View.(tango.View)
		if !ok || view == nil {
			return nil, fmt.Errorf("%w: %q has no tango.View (use admin.RegisterPage)", ErrInvalidPage, entry.Path)
		}
		mounted = append(mounted, mountedPage{entry: entry, view: view})
	}
	return mounted, nil
}

// pageRoutes mounts one page for GET and POST behind the session gate; every
// other method gets the admin's method-not-allowed page. A POST must carry the
// admin's CSRF token, as the admin's own forms do.
func pageRoutes(mounted mountedPage, store *db.Store, page, brand chrome, branding Branding) tango.URLs {
	path := pagePath(mounted.entry.Path)
	current := page
	current.Active = pageNavKey(mounted.entry.Path)
	gated := htmlPage(brand, requireSession(store, current, guardPagePost(mounted.view)))
	routes := tango.URLs{
		tango.Path(http.MethodGet, path, gated, hiddenPage),
		tango.Path(http.MethodPost, path, gated, hiddenPage),
	}
	return append(routes, unhandledMethodRoutes(path, branding, http.MethodGet, http.MethodPost)...)
}

// guardPagePost refuses a POST that lacks the session's CSRF token before the
// host's View runs.
func guardPagePost(view tango.View) tango.View {
	return func(ctx *tango.Context) error {
		if ctx.Request().Method == http.MethodPost && !verifySessionCSRF(ctx) {
			return forbiddenCSRF(ctx)
		}
		return view(ctx)
	}
}

// CurrentUser returns the username of the staff account a page View is serving,
// and false in a View that is not behind the admin's session gate. It exposes
// the username only, not the AdminUser model.
func CurrentUser(ctx *tango.Context) (string, bool) {
	who := signedInFrom(ctx.Context())
	if who == nil {
		return "", false
	}
	return who.Username, true
}

// CSRFToken returns the token a page's form must post as "csrf_token", or ""
// outside the admin's session gate.
func CSRFToken(ctx *tango.Context) string {
	if who := signedInFrom(ctx.Context()); who != nil {
		return who.CSRFToken
	}
	return ""
}

type pageData struct {
	chrome
	Title string
	Body  template.HTML
}

var pageTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.Title}}</h1>
  </div>
</header>
<div class="content-body">{{.Body}}</div>
{{end}}`)

// RenderPage answers a page View with body inside the admin's layout:
// navigation, theme, notices and the content security policy nonce. title is
// the heading and the document title. body is HTML the host has already
// escaped.
func RenderPage(ctx *tango.Context, title string, body template.HTML) error {
	return render(ctx, http.StatusOK, pageTemplate, pageData{
		chrome: chromeFrom(ctx.Context()).withContext(ctx.Context()).withPageTitle(title),
		Title:  title,
		Body:   body,
	})
}

package admin

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/internal/security"
	"github.com/angvp/tango/model"
)

// Options controls how a model appears in the admin.
type Options = adminregistry.Options

// ModelRegistration is the admin metadata for a registered model.
type ModelRegistration = adminregistry.ModelRegistration

// Registry stores admin registrations by model name.
type Registry = adminregistry.Registry

// NewRegistry returns an empty, ready-to-use admin Registry.
var NewRegistry = adminregistry.NewRegistry

// New constructs the admin application. Authentication is session-cookie
// based, against Admin accounts managed by the "tango admin" CLI family
// (create/resetpassword/deactivate) — there is no static credential value
// to pass in here. opts configures optional admin-wide behavior, currently
// WithBranding and WithMiddleware; admin.New(store) with no options is
// unchanged from before Option existed.
func New(store *db.Store, opts ...Option) tango.App {
	cfg := adminConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	state := &installState{}
	inner := tango.NewApp("admin", func(registry *tango.Registry) error {
		state.registry = registry
		registry.SetStore(store)

		if err := registry.Models().Register(AdminUser{}); err != nil {
			return err
		}
		if err := registry.Models().Register(AdminSession{}); err != nil {
			return err
		}

		registrations := registry.Admin().Registrations()
		sort.Slice(registrations, func(i, j int) bool {
			return registrations[i].Model.Name < registrations[j].Model.Name
		})
		state.installed = make(map[string]struct{}, len(registrations))
		for _, registration := range registrations {
			state.installed[registration.Model.Name] = struct{}{}
		}

		nav := make([]navItem, len(registrations))
		for i, registration := range registrations {
			nav[i] = navItem{
				Name:  registration.Model.Name,
				Label: sectionName(registration.Model, registration.Options),
				Path:  "/admin/" + db.ColumnName(registration.Model.Name) + "/",
			}
		}

		limiter := security.NewRateLimiter(loginRateLimitAttempts, loginRateLimitWindow)
		clientKey := security.ClientKey(cfg.trustedProxies)
		landing := indexView(nav, cfg.branding)
		if cfg.dashboard.enabled() {
			landing = dashboardView(nav, cfg.branding, cfg.dashboard)
		}
		page := pageChrome{nav: nav, brand: cfg.branding}
		index := htmlPage(page, requireSession(store, landing))
		routes := tango.URLs{
			tango.Path(http.MethodGet, "/admin/", index, tango.Name("index"), hiddenPage),
			// Also match "/admin" (no trailing slash) directly, rather than
			// relying on a redirect: chi treats the two as distinct routes,
			// and Include's own slash-normalization only applies to prefixes
			// passed to Include, not to a literal pattern like this one.
			tango.Path(http.MethodGet, "/admin", index, hiddenPage),
			tango.Path(http.MethodGet, "/admin/login/", htmlPage(pageChrome{brand: cfg.branding}, loginView(store, limiter, clientKey)), tango.Name("login"), hiddenPage),
			tango.Path(http.MethodPost, "/admin/login/", htmlPage(pageChrome{brand: cfg.branding}, loginView(store, limiter, clientKey)), hiddenPage),
			tango.Path(http.MethodPost, "/admin/logout/", htmlPage(pageChrome{brand: cfg.branding}, logoutView(store)), tango.Name("logout"), hiddenPage),
			tango.Path(http.MethodPost, "/admin/theme/", htmlPage(page, requireSession(store, themeView())), hiddenPage),
			tango.Path(http.MethodPost, "/admin/messages/dismiss/", htmlPage(page, requireSession(store, dismissView())), hiddenPage),
			tango.Path(http.MethodPost, densityBasePath, htmlPage(page, requireSession(store, densityView())), hiddenPage),
		}
		routes = append(routes, assetRoutes()...)
		for _, registration := range registrations {
			modelPath := modelAdminPath(registration.Model.Name)
			routes = append(routes, modelRoutes(modelPath, store, registry.Models(), registry.Admin(), registration, nav, cfg.branding)...)
		}

		middleware := append([]tango.Middleware{withThemeState}, cfg.middleware...)
		return registry.Routes().Include("/", routes, tango.WithMiddleware(middleware...))
	})
	return checkedApp{App: inner, state: state}
}

// installState remembers what the admin saw when it was installed, so Checks
// can tell whether models were registered with it afterwards.
type installState struct {
	registry  *tango.Registry
	installed map[string]struct{}
}

// checkedApp is the admin app plus the install-order check.
type checkedApp struct {
	tango.App
	state *installState
}

// Checks reports models registered with the admin after the admin was
// installed. The admin builds its routes and navigation when installed, so
// those models would be missing from it without any error.
func (a checkedApp) Checks() []tango.AppCheck {
	const description = "admin lists every model registered with it"
	if a.state.registry == nil {
		return nil
	}
	var late []string
	for _, registration := range a.state.registry.Admin().Registrations() {
		if _, ok := a.state.installed[registration.Model.Name]; !ok {
			late = append(late, registration.Model.Name)
		}
	}
	if len(late) == 0 {
		return []tango.AppCheck{{Description: description}}
	}
	sort.Strings(late)
	return []tango.AppCheck{{Description: description, Err: fmt.Errorf(
		"%s registered after admin.New was installed and will not appear in the admin; list admin.New last in InstalledApps",
		strings.Join(late, ", "))}}
}

func modelRoutes(modelPath string, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.URLs {
	page := pageChrome{nav: nav, brand: brand}
	list := htmlPage(page, requireSession(store, listView(store, models, adminReg, registration, nav, brand)))
	create := htmlPage(page, requireSession(store, createView(store, models, adminReg, registration, nav, brand)))
	edit := htmlPage(page, requireSession(store, editView(store, models, adminReg, registration, nav, brand)))
	del := htmlPage(page, requireSession(store, deleteView(store, models, adminReg, registration, nav, brand)))

	urls := tango.URLs{
		tango.Path(http.MethodGet, modelPath, list, hiddenPage),
		tango.Path(http.MethodGet, modelPath+"new/", create, hiddenPage),
		tango.Path(http.MethodPost, modelPath+"new/", create, hiddenPage),
		tango.Path(http.MethodGet, modelPath+"{pk}/", edit, hiddenPage),
		tango.Path(http.MethodPost, modelPath+"{pk}/", edit, hiddenPage),
		tango.Path(http.MethodGet, modelPath+"{pk}/delete/", del, hiddenPage),
		tango.Path(http.MethodPost, modelPath+"{pk}/delete/", del, hiddenPage),
	}
	if registration.Options.BulkDelete {
		bulk := htmlPage(page, requireSession(store, bulkDeleteView(store, registration, nav, brand)))
		urls = append(urls, tango.Path(http.MethodPost, modelPath+"delete-selected/", bulk, hiddenPage))
	}
	return append(urls, previewRoutes(modelPath, store, registration)...)
}

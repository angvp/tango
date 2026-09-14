package admin

import (
	"net/http"
	"sort"

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

	return tango.NewApp("admin", func(registry *tango.Registry) error {
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

		nav := make([]navItem, len(registrations))
		for i, registration := range registrations {
			nav[i] = navItem{
				Name: registration.Model.Name,
				Path: "/admin/" + db.ColumnName(registration.Model.Name) + "/",
			}
		}

		limiter := security.NewRateLimiter(loginRateLimitAttempts, loginRateLimitWindow)
		index := requireSession(store, indexView(nav, cfg.branding))
		routes := tango.URLs{
			tango.Path(http.MethodGet, "/admin/", index, tango.Name("index")),
			// Also match "/admin" (no trailing slash) directly, rather than
			// relying on a redirect: chi treats the two as distinct routes,
			// and Include's own slash-normalization only applies to prefixes
			// passed to Include, not to a literal pattern like this one.
			tango.Path(http.MethodGet, "/admin", index),
			tango.Path(http.MethodGet, "/admin/login/", loginView(store, limiter), tango.Name("login")),
			tango.Path(http.MethodPost, "/admin/login/", loginView(store, limiter)),
			tango.Path(http.MethodPost, "/admin/logout/", logoutView(store), tango.Name("logout")),
		}
		for _, registration := range registrations {
			modelPath := "/admin/" + db.ColumnName(registration.Model.Name) + "/"
			routes = append(routes, modelRoutes(modelPath, store, registry.Models(), registry.Admin(), registration, nav, cfg.branding)...)
		}

		return registry.Routes().Include("/", routes, tango.WithMiddleware(cfg.middleware...))
	})
}

func modelRoutes(modelPath string, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.URLs {
	list := requireSession(store, listView(store, models, adminReg, registration, nav, brand))
	create := requireSession(store, createView(store, models, adminReg, registration, nav, brand))
	edit := requireSession(store, editView(store, models, adminReg, registration, nav, brand))
	del := requireSession(store, deleteView(store, registration, nav, brand))

	return tango.URLs{
		tango.Path(http.MethodGet, modelPath, list),
		tango.Path(http.MethodGet, modelPath+"new/", create),
		tango.Path(http.MethodPost, modelPath+"new/", create),
		tango.Path(http.MethodGet, modelPath+"{pk}/", edit),
		tango.Path(http.MethodPost, modelPath+"{pk}/", edit),
		tango.Path(http.MethodGet, modelPath+"{pk}/delete/", del),
		tango.Path(http.MethodPost, modelPath+"{pk}/delete/", del),
	}
}

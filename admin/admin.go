package admin

import (
	"net/http"
	"sort"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
)

// Options controls how a model appears in the admin.
type Options = adminregistry.Options

// ModelRegistration is the admin metadata for a registered model.
type ModelRegistration = adminregistry.ModelRegistration

// Registry stores admin registrations by model name.
type Registry = adminregistry.Registry

// NewRegistry returns an empty, ready-to-use admin Registry.
var NewRegistry = adminregistry.NewRegistry

// Credentials are the HTTP Basic Auth credentials for admin routes.
type Credentials struct {
	Username string
	Password string
}

// New constructs the admin application.
func New(store *db.Store, credentials Credentials) tango.App {
	return tango.NewApp("admin", func(registry *tango.Registry) error {
		registry.SetStore(store)

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

		index := protectedView(credentials, indexView(nav))
		routes := tango.URLs{
			tango.Path(http.MethodGet, "/admin/", index, tango.Name("index")),
			// Also match "/admin" (no trailing slash) directly, rather than
			// relying on a redirect: chi treats the two as distinct routes,
			// and Include's own slash-normalization only applies to prefixes
			// passed to Include, not to a literal pattern like this one.
			tango.Path(http.MethodGet, "/admin", index),
		}
		for _, registration := range registrations {
			modelPath := "/admin/" + db.ColumnName(registration.Model.Name) + "/"
			routes = append(routes, modelRoutes(modelPath, credentials, store, registration, nav)...)
		}

		return registry.Routes().Include("/", routes)
	})
}

func modelRoutes(modelPath string, credentials Credentials, store *db.Store, registration ModelRegistration, nav []navItem) tango.URLs {
	list := protectedView(credentials, listView(store, registration, nav))
	create := protectedView(credentials, createView(store, registration, nav))
	edit := protectedView(credentials, editView(store, registration, nav))
	del := protectedView(credentials, deleteView(store, registration, nav))

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

func protectedView(credentials Credentials, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		username, password, ok := ctx.Request().BasicAuth()
		if !ok || username != credentials.Username || password != credentials.Password {
			ctx.ResponseWriter().Header().Set("WWW-Authenticate", `Basic realm="tanGO admin"`)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
		}

		return next(ctx)
	}
}

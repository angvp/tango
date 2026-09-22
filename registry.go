package tango

import (
	"errors"
	"fmt"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// ErrDuplicateApp is returned by Registry.Register when an app with the same
// Name() has already been registered.
var ErrDuplicateApp = errors.New("tango: duplicate app name")

// Registry is the shared structure apps register into during boot.
type Registry struct {
	apps             []App
	appNames         map[string]struct{}
	admin            *adminregistry.Registry
	models           *model.Registry
	routes           *RouteRegistry
	registrationDone bool
	store            *db.Store
	lifecycles       []Lifecycle
	lifecycleNames   map[string]struct{}
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		appNames: make(map[string]struct{}),
	}
}

// Register adds app to the registry. It returns an error wrapping
// ErrDuplicateApp if an app with the same Name() has already been
// registered, without invoking any app's Register callback.
func (r *Registry) Register(app App) error {
	name := app.Name()

	if _, exists := r.appNames[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateApp, name)
	}

	r.appNames[name] = struct{}{}
	r.apps = append(r.apps, app)

	return nil
}

// RunRegistration invokes each registered app's Register callback exactly
// once, in the order the apps were added, passing this Registry to each.
// It stops and returns the first error encountered, and does not invoke
// any subsequent app's callback.
func (r *Registry) RunRegistration() error {
	for _, app := range r.apps {
		r.Models().SetCurrentApp(app.Name())
		if err := app.Register(r); err != nil {
			return err
		}
	}

	r.registrationDone = true

	return nil
}

// Models returns the model sub-registry, creating it on first use.
func (r *Registry) Models() *model.Registry {
	if r.models == nil {
		r.models = model.NewRegistry()
	}
	return r.models
}

// SetStore stores the persistence store for apps that need database access.
// It also wires this Registry's model registry into store (store.UseModels),
// so Delete cascades across every registered model's foreign keys with no
// extra call needed by app code — see db.Store.UseModels.
func (r *Registry) SetStore(store *db.Store) {
	store.UseModels(r.Models())
	r.store = store
}

// Store returns the configured persistence store, if one has been set.
func (r *Registry) Store() (*db.Store, bool) {
	if r.store == nil {
		return nil, false
	}
	return r.store, true
}

// Admin returns the admin sub-registry, creating it on first use.
func (r *Registry) Admin() *adminregistry.Registry {
	if r.admin == nil {
		r.admin = adminregistry.NewRegistry(r.Models())
	}
	return r.admin
}

// Checks returns every advisory AppCheck contributed by installed apps that
// implement Checker, in InstalledApps order. Apps that do not implement
// Checker are skipped without error. Checks is only meaningful after
// RunRegistration has completed.
func (r *Registry) Checks() []AppCheck {
	var checks []AppCheck
	for _, app := range r.apps {
		checker, ok := app.(Checker)
		if !ok {
			continue
		}
		checks = append(checks, checker.Checks()...)
	}
	return checks
}

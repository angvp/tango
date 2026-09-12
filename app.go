package tango

// App is the narrow contract every tanGO application unit satisfies. This
// interface is stable: no breaking changes are planned, and apps are not
// expected to implement it by hand (use NewApp).
//
// A distributable third-party app package should expose one exported App
// value or constructor at its package root, e.g.:
//
//	var App = tango.NewApp("widgets", registerWidgets)
//
// or, if the app needs configuration:
//
//	func New(cfg Config) tango.App { ... }
//
// Importers then reference it directly: InstalledApps: []tango.App{widgets.App}
// or InstalledApps: []tango.App{widgets.New(cfg)}. No other framework
// integration is required.
//
// An App may optionally also implement Checker to contribute advisory
// checks; it is never required to.
//
// An app that ships its own templates or static assets embeds and mounts
// them itself, via its own embed.FS and a route registered through
// Include/Routes() inside its Register callback — the same pattern the
// admin package uses. There is no separate framework-level mechanism for
// this.
type App interface {
	Name() string
	Register(*Registry) error
}

type app struct {
	name       string
	registerFn func(*Registry) error
}

// NewApp constructs an App from a name and registration function.
func NewApp(name string, registerFn func(*Registry) error) App {
	return app{
		name:       name,
		registerFn: registerFn,
	}
}

func (a app) Name() string {
	return a.name
}

func (a app) Register(registry *Registry) error {
	return a.registerFn(registry)
}

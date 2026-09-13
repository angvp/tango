package tango

// URLs is a plain list of Route values.
type URLs []Route

// Route declares how an HTTP method and path pattern map to a View.
type Route struct {
	method     string
	pattern    string
	view       View
	name       string
	middleware []Middleware
}

// RouteOption customizes a Route during construction.
type RouteOption func(*Route)

// Path constructs a Route declaration without executing its View.
func Path(method, pattern string, view View, opts ...RouteOption) Route {
	route := Route{
		method:  method,
		pattern: pattern,
		view:    view,
	}

	for _, opt := range opts {
		opt(&route)
	}

	return route
}

// Name attaches a name to a Route declaration.
func Name(name string) RouteOption {
	return func(route *Route) {
		route.name = name
	}
}

// Use attaches Middleware to one Route. Middleware runs before *Context is
// constructed; the first middleware passed is outermost.
func Use(middleware ...Middleware) RouteOption {
	return func(route *Route) {
		route.middleware = append(route.middleware, middleware...)
	}
}

// Method returns the route's HTTP method.
func (r Route) Method() string {
	return r.method
}

// Pattern returns the route's path pattern.
func (r Route) Pattern() string {
	return r.pattern
}

// View returns the route's view.
func (r Route) View() View {
	return r.view
}

// Name returns the route's optional name.
func (r Route) Name() string {
	return r.name
}

// Middleware returns the route-scoped Middleware attached to this Route.
func (r Route) Middleware() []Middleware {
	return append([]Middleware(nil), r.middleware...)
}

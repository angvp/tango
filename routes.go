package tango

// URLs is a plain list of Route values.
type URLs []Route

// Route declares how an HTTP method and path pattern map to a View.
type Route struct {
	method  string
	pattern string
	view    View
	name    string
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

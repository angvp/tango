package tango

// RouteDoc describes one route for tools that read the route table, such as
// the openapi package. Every field is optional; a route without a RouteDoc is
// still described by its method, path and name.
//
// Body, Query and the values of Responses are sample values whose Go types
// describe the shape: pass a zero value or a pointer to one (for example
// Pet{} or []Pet{}), never a pointer-to-pointer. A nil Responses value means
// the response has no body.
type RouteDoc struct {
	// Summary is one line naming what the route does.
	Summary string
	// Description is longer text; Markdown is allowed.
	Description string
	// Tags group the route. Without any, the route is tagged with the first
	// segment of the prefix it was included under.
	Tags []string
	// Body is the request body's type, sent as JSON.
	Body any
	// Query is a struct whose fields are the query parameters. A field is
	// named by its `query:"name"` tag, or its lower-cased Go name, and is
	// required when its tag says `query:"name,required"`.
	Query any
	// Responses maps an HTTP status to the response body's type.
	Responses map[int]any
	// Bearer marks the route as needing an `Authorization: Bearer` token.
	Bearer bool
	// Deprecated marks the route as deprecated.
	Deprecated bool
	// Hidden leaves the route out of generated documentation.
	Hidden bool
}

// Doc attaches a RouteDoc to a Route declaration.
func Doc(doc RouteDoc) RouteOption {
	return func(route *Route) {
		route.doc = &doc
	}
}

// DescribedRoute is one mounted route as tools see it.
type DescribedRoute struct {
	Method string
	// Pattern is the full mounted path pattern, with chi placeholders such
	// as {id} or {id:[0-9]+}.
	Pattern string
	// Name is the fully-qualified route name, or "".
	Name string
	// Prefix is the first path segment of the prefix the route was included
	// under, or "".
	Prefix string
	// Doc is the route's RouteDoc, or nil.
	Doc *RouteDoc
}

// Described returns every route included so far, in inclusion order, for
// tools that document or audit the route table. The returned values are
// copies.
func (rr *RouteRegistry) Described() []DescribedRoute {
	out := make([]DescribedRoute, 0, len(rr.included))
	for _, route := range rr.included {
		described := DescribedRoute{
			Method:  route.method,
			Pattern: route.pattern,
			Name:    route.qualifiedName,
			Prefix:  route.prefix,
		}
		if route.doc != nil {
			doc := *route.doc
			doc.Tags = append([]string(nil), doc.Tags...)
			described.Doc = &doc
		}
		out = append(out, described)
	}
	return out
}

package tango

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ErrMalformedPattern is returned when a route pattern has unbalanced
// parameter placeholders (e.g. an unclosed "{").
var ErrMalformedPattern = errors.New("tango: malformed route pattern")

// ErrDuplicateRouteName is returned when two routes resolve to the same
// fully-qualified name.
var ErrDuplicateRouteName = errors.New("tango: duplicate route name")

// ErrRegistrationNotComplete is returned by Handler and Reverser when
// Registry.RunRegistration has not yet completed.
var ErrRegistrationNotComplete = errors.New("tango: registration not complete")

// includedRoute is a Route after it has been mounted under a prefix and had
// its name namespace-qualified by Include.
type includedRoute struct {
	method        string
	pattern       string
	view          View
	qualifiedName string
	middleware    []Middleware
}

// RouteRegistry is the Milestone 2 sub-registry apps contribute routes to,
// mirroring the shape of Milestone 1's Registry sub-APIs.
type RouteRegistry struct {
	registry   *Registry
	included   []includedRoute
	middleware []Middleware
}

// IncludeOption customizes one Include call.
type IncludeOption func(*includeConfig)

type includeConfig struct {
	middleware []Middleware
}

// WithMiddleware attaches Middleware to every Route mounted by one Include
// call. Middleware runs before *Context is constructed; the first middleware
// passed is outermost.
func WithMiddleware(middleware ...Middleware) IncludeOption {
	return func(config *includeConfig) {
		config.middleware = append(config.middleware, middleware...)
	}
}

// Routes returns the route sub-registry, creating it on first use.
func (r *Registry) Routes() *RouteRegistry {
	if r.routes == nil {
		r.routes = &RouteRegistry{registry: r}
	}
	return r.routes
}

// Include mounts routes under prefix, namespace-qualifying any named route
// and normalizing trailing slashes so Include("/users", ...) and
// Include("/users/", ...) behave identically. It fails fast on a malformed
// pattern or a duplicate fully-qualified name within this call's routes,
// registering none of them in that case. Cross-call duplicate detection is
// deferred to Handler/Reverser compile time.
func (rr *RouteRegistry) Include(prefix string, routes []Route, opts ...IncludeOption) error {
	config := includeConfig{}
	for _, opt := range opts {
		opt(&config)
	}
	namespace := strings.Trim(prefix, "/")

	resolved := make([]includedRoute, 0, len(routes))
	seenNames := make(map[string]struct{}, len(routes))

	for _, route := range routes {
		if err := validatePattern(route.Pattern()); err != nil {
			return err
		}

		qualifiedName := ""
		if name := route.Name(); name != "" {
			if namespace != "" {
				qualifiedName = namespace + ":" + name
			} else {
				qualifiedName = name
			}

			if _, exists := seenNames[qualifiedName]; exists {
				return fmt.Errorf("%w: %q", ErrDuplicateRouteName, qualifiedName)
			}
			seenNames[qualifiedName] = struct{}{}
		}

		resolved = append(resolved, includedRoute{
			method:        route.Method(),
			pattern:       joinPath(prefix, route.Pattern()),
			view:          route.View(),
			qualifiedName: qualifiedName,
			middleware:    append(append([]Middleware(nil), config.middleware...), route.Middleware()...),
		})
	}

	rr.included = append(rr.included, resolved...)

	return nil
}

// compiled is the result of resolving the full route tree: a plain
// http.Handler plus the name->pattern map Reverser needs, computed once from
// the same included-route data Handler and Reverser both depend on.
type compiled struct {
	handler  http.Handler
	patterns map[string]string
}

// compile builds the chi mux and detects fully-qualified route-name
// collisions between routes contributed by different Include calls (the
// cross-app case Include itself cannot see).
func (rr *RouteRegistry) compile() (*compiled, error) {
	mux := chi.NewRouter()
	patterns := make(map[string]string, len(rr.included))

	for _, route := range rr.included {
		if route.qualifiedName != "" {
			if _, exists := patterns[route.qualifiedName]; exists {
				return nil, fmt.Errorf("%w: %q", ErrDuplicateRouteName, route.qualifiedName)
			}
			patterns[route.qualifiedName] = route.pattern
		}

		view := route.view
		terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			params := make(map[string]string)
			if chiCtx := chi.RouteContext(r.Context()); chiCtx != nil {
				for i, key := range chiCtx.URLParams.Keys {
					params[key] = chiCtx.URLParams.Values[i]
				}
			}

			ctx := newContext(w, r, params)
			if err := view(ctx); err != nil {
				log.Printf("tango: view error: %v", err)
				writeInternalError(w)
			}
		})

		handler := applyMiddleware(terminal, append(append([]Middleware(nil), rr.middleware...), route.middleware...))
		mux.Method(route.method, route.pattern, handler)
	}

	return &compiled{handler: mux, patterns: patterns}, nil
}

func (rr *RouteRegistry) setMiddleware(middleware []Middleware) {
	rr.middleware = append([]Middleware(nil), middleware...)
}

// Handler compiles the full route tree, contributed by every app's Include
// call, into a plain http.Handler. It returns ErrRegistrationNotComplete if
// Registry.RunRegistration has not yet completed, and a non-nil error if two
// different Include calls produced the same fully-qualified route name.
func (rr *RouteRegistry) Handler() (http.Handler, error) {
	if !rr.registry.registrationDone {
		return nil, ErrRegistrationNotComplete
	}

	compiled, err := rr.compile()
	if err != nil {
		return nil, err
	}

	return compiled.handler, nil
}

// Params holds substitution values for a reverse URL lookup.
type Params map[string]string

// Reverser builds a URL for a fully-qualified route name.
type Reverser interface {
	Reverse(name string, params Params) (string, error)
}

// ErrUnknownRouteName is returned by Reverse for a route name that was
// never registered.
var ErrUnknownRouteName = errors.New("tango: unknown route name")

// ErrMissingParam is returned by Reverse when a pattern's placeholder has
// no corresponding entry in params.
var ErrMissingParam = errors.New("tango: missing route param")

type reverser struct {
	patterns map[string]string
}

// Reverse builds the URL for name, substituting params into its pattern's
// placeholders. name must be fully-qualified; there is no local-name form.
func (rv *reverser) Reverse(name string, params Params) (string, error) {
	pattern, exists := rv.patterns[name]
	if !exists {
		return "", fmt.Errorf("%w: %q", ErrUnknownRouteName, name)
	}

	var b strings.Builder
	i := 0
	for i < len(pattern) {
		if pattern[i] != '{' {
			b.WriteByte(pattern[i])
			i++
			continue
		}

		end := strings.IndexByte(pattern[i:], '}')
		if end == -1 {
			return "", fmt.Errorf("%w: %q", ErrMalformedPattern, pattern)
		}
		paramName := pattern[i+1 : i+end]
		value, ok := params[paramName]
		if !ok {
			return "", fmt.Errorf("%w: %q in route %q", ErrMissingParam, paramName, name)
		}
		b.WriteString(value)
		i += end + 1
	}

	return b.String(), nil
}

// Reverser compiles the full route tree (the same compilation Handler
// performs) and returns a Reverser for building URLs from route names. It
// returns ErrRegistrationNotComplete if Registry.RunRegistration has not
// yet completed.
func (rr *RouteRegistry) Reverser() (Reverser, error) {
	if !rr.registry.registrationDone {
		return nil, ErrRegistrationNotComplete
	}

	compiled, err := rr.compile()
	if err != nil {
		return nil, err
	}

	return &reverser{patterns: compiled.patterns}, nil
}

// IncludedNames returns the fully-qualified names of every named route
// included so far, in inclusion order. It exists to make Include's
// namespacing/normalization behavior testable before Handler/Reverser land.
func (rr *RouteRegistry) IncludedNames() []string {
	names := make([]string, 0, len(rr.included))
	for _, route := range rr.included {
		if route.qualifiedName != "" {
			names = append(names, route.qualifiedName)
		}
	}
	return names
}

// IncludedPatterns returns the mounted path pattern of every route included
// so far, in inclusion order.
func (rr *RouteRegistry) IncludedPatterns() []string {
	patterns := make([]string, 0, len(rr.included))
	for _, route := range rr.included {
		patterns = append(patterns, route.pattern)
	}
	return patterns
}

// joinPath mounts pattern under prefix without producing doubled or missing
// slashes, regardless of whether prefix or pattern already carry leading or
// trailing slashes.
func joinPath(prefix, pattern string) string {
	trimmedPrefix := strings.TrimRight(prefix, "/")
	trimmedPattern := "/" + strings.TrimLeft(pattern, "/")

	if trimmedPrefix == "" {
		return trimmedPattern
	}

	return trimmedPrefix + trimmedPattern
}

// validatePattern checks that a route pattern's "{" and "}" placeholders are
// balanced.
func validatePattern(pattern string) error {
	depth := 0
	for _, r := range pattern {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("%w: %q", ErrMalformedPattern, pattern)
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("%w: %q", ErrMalformedPattern, pattern)
	}
	return nil
}

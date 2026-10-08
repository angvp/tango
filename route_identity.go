package tango

import (
	"context"
	"net/http"
)

// routeIdentityKey is the request-context key of a request's route
// identity: the pattern of the route that answers it.
type routeIdentityKey struct{}

// withRouteIdentity records route as r's route identity. It is recorded
// once, outermost, so every layer that reports a route (Context.Logger,
// the observability middleware, automatic metrics and View-error logging)
// reports the same one.
func withRouteIdentity(r *http.Request, route string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), routeIdentityKey{}, route))
}

// routeIdentity returns r's route identity, or "" if none was recorded.
func routeIdentity(r *http.Request) string {
	route, _ := r.Context().Value(routeIdentityKey{}).(string)
	return route
}

// identifyRoute records route as the identity of every request next
// handles.
func identifyRoute(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, withRouteIdentity(r, route))
	})
}

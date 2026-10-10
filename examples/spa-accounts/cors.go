package main

import (
	"net/http"

	"github.com/angvp/tango"
)

// cors lets the single-page client at origin call the API from a browser.
// CORS is the host's concern, not accounts': tanGO ships no CORS package, so
// the application decides which origin may call it. Registered with
// MiddlewareScopeAll so a preflight OPTIONS request, which matches no route,
// is answered too.
func cors(origin string) tango.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Origin") == origin {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

package tango

import (
	"encoding/json"
	"log"
	"net/http"
)

// Middleware wraps a raw net/http handler before a *tango.Context exists.
// Use it for cross-cutting HTTP concerns such as recovery, logging, request
// IDs, CORS, or compression. Context-aware behavior such as auth redirects
// belongs in a View wrapper instead.
type Middleware func(http.Handler) http.Handler

// Recoverer returns middleware that converts downstream panics into the same
// generic JSON 500 response tanGO uses for Views that return errors.
func Recoverer() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("tango: view error: %v", recovered)
					writeInternalError(w)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func writeInternalError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
}

func applyMiddleware(handler http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

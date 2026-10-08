package tango

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// MaxBodySize returns Middleware that caps request bodies at n bytes. A
// request whose Content-Length already exceeds n gets 413 before the View
// runs. Otherwise the body is limited with http.MaxBytesReader: a View
// that reads past the limit gets an *http.MaxBytesError, which, if the View
// returns it unhandled, is answered with 413 instead of the generic 500. A
// View that handles the error itself keeps control of its response.
//
// When more than one body limit applies to a request, the most restrictive
// wins: middleware runs outermost first, so a limit closer to the View can
// keep or lower an outer one, never raise it. Leave a route that needs
// larger bodies outside the stricter limit, for example in its own route
// group. n must be positive; MaxBodySize panics otherwise.
func MaxBodySize(n int64) Middleware {
	if n <= 0 {
		panic(fmt.Sprintf("tango: MaxBodySize needs a positive size, got %d", n))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				writeBodyTooLarge(w)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isBodyTooLarge reports whether err is a body limit's rejection.
func isBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

func writeBodyTooLarge(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "request body too large"})
}

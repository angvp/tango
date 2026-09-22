package jwt

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/angvp/tango"
)

type claimsContextKey struct{}

// Extractor obtains a JWT string from one request.
type Extractor func(*http.Request) (string, error)

// BearerToken reads an Authorization: Bearer token header.
func BearerToken(request *http.Request) (string, error) {
	if request == nil {
		return "", ErrMissingToken
	}
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrMissingToken
	}
	return parts[1], nil
}

// QueryToken returns an Extractor that reads a named query parameter.
func QueryToken(name string) Extractor {
	return func(request *http.Request) (string, error) {
		if request == nil || name == "" {
			return "", ErrMissingToken
		}
		token := request.URL.Query().Get(name)
		if token == "" {
			return "", ErrMissingToken
		}
		return token, nil
	}
}

// Middleware verifies an extracted token once per request and stores Claims
// on the standard request context. Missing tokens pass through for optional
// authentication; supplied invalid tokens receive a generic JSON 401.
func (s *Service) Middleware(extract Extractor) tango.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if extract == nil {
				writeUnauthorized(writer)
				return
			}
			encoded, err := extract(request)
			if errors.Is(err, ErrMissingToken) {
				next.ServeHTTP(writer, request)
				return
			}
			if err != nil {
				writeUnauthorized(writer)
				return
			}
			claims, err := s.Verify(encoded)
			if err != nil {
				writeUnauthorized(writer)
				return
			}
			ctx := context.WithValue(request.Context(), claimsContextKey{}, claims)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

// FromContext returns the JWT Claims installed by Service.Middleware.
func FromContext(ctx context.Context) (Claims, bool) {
	if ctx == nil {
		return Claims{}, false
	}
	claims, ok := ctx.Value(claimsContextKey{}).(Claims)
	return claims, ok
}

// Require rejects requests without Claims installed by Service.Middleware.
// If no JWT middleware runs before this View wrapper, it always returns 401.
func Require(next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		if _, ok := FromContext(ctx.Context()); !ok {
			writeUnauthorized(ctx.ResponseWriter())
			return nil
		}
		return next(ctx)
	}
}

func writeUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusUnauthorized)
	_, _ = writer.Write([]byte("{\"error\":\"unauthorized\"}\n"))
}

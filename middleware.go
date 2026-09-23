package tango

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
	"unicode"

	"github.com/angvp/tango/internal/observabilitysafe"
	"github.com/angvp/tango/observability"
	"github.com/go-chi/chi/v5"
)

// Middleware wraps a raw net/http handler before a *tango.Context exists.
// Use it for cross-cutting HTTP concerns such as recovery, logging, request
// IDs, CORS, or compression. Context-aware behavior such as auth redirects
// belongs in a View wrapper instead.
type Middleware func(http.Handler) http.Handler

// Recoverer returns middleware that converts downstream panics into the same
// generic JSON 500 response tanGO uses for Views that return errors.
type recoveryConfig struct{ logger *slog.Logger }

// RecoveryOption configures Recoverer.
type RecoveryOption func(*recoveryConfig)

// WithRecoveryLogger configures Recoverer's logger. Nil uses slog.Default.
func WithRecoveryLogger(logger *slog.Logger) RecoveryOption {
	return func(config *recoveryConfig) { config.logger = logger }
}

func Recoverer(opts ...RecoveryOption) Middleware {
	config := recoveryConfig{logger: slog.Default()}
	for _, opt := range opts {
		opt(&config)
	}
	if config.logger == nil {
		config.logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					attrs := requestAttrs(r)
					attrs = append(attrs, slog.Any("recovered", recovered), slog.String("stack", string(debug.Stack())))
					observabilitysafe.Call(func() {
						config.logger.LogAttrs(r.Context(), slog.LevelError, EventPanic, attrs...)
					})
					writeInternalError(w)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type requestIDContextKey struct{}

type requestIDConfig struct {
	logger        *slog.Logger
	trustedHeader string
}

// RequestIDOption configures RequestID.
type RequestIDOption func(*requestIDConfig)

// WithRequestIDLogger configures RequestID's failure logger. Nil uses slog.Default.
func WithRequestIDLogger(logger *slog.Logger) RequestIDOption {
	return func(config *requestIDConfig) { config.logger = logger }
}

// WithTrustedHeader allows RequestID to accept a validated ID from name.
// It panics when name is not a valid HTTP field name.
func WithTrustedHeader(name string) RequestIDOption {
	if !validHeaderName(name) {
		panic(fmt.Sprintf("tango: invalid trusted request ID header %q", name))
	}
	canonical := http.CanonicalHeaderKey(name)
	return func(config *requestIDConfig) { config.trustedHeader = canonical }
}

var generateRequestID = func() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// RequestID attaches a correlation ID to the request context and response.
func RequestID(opts ...RequestIDOption) Middleware {
	config := requestIDConfig{logger: slog.Default()}
	for _, opt := range opts {
		opt(&config)
	}
	if config.logger == nil {
		config.logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := ""
			if config.trustedHeader != "" {
				candidate := r.Header.Get(config.trustedHeader)
				if validRequestID(candidate) {
					requestID = candidate
				}
			}
			if requestID == "" {
				generated, err := generateRequestID()
				if err != nil {
					attrs := requestAttrs(r)
					attrs = append(attrs, slog.Any("error", err))
					observabilitysafe.Call(func() {
						config.logger.LogAttrs(r.Context(), slog.LevelWarn, EventRequestIDGenerationFailed, attrs...)
					})
					next.ServeHTTP(w, r)
					return
				}
				requestID = generated
			}
			w.Header().Set("X-Request-ID", requestID)
			r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDFromContext returns the correlation ID installed by RequestID.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDContextKey{}).(string)
	return requestID, ok && requestID != ""
}

type accessLogConfig struct{ logger *slog.Logger }

// AccessLogOption configures AccessLogger.
type AccessLogOption func(*accessLogConfig)

// WithAccessLogger configures AccessLogger's logger. Nil uses slog.Default.
func WithAccessLogger(logger *slog.Logger) AccessLogOption {
	return func(config *accessLogConfig) { config.logger = logger }
}

// AccessLogger logs one structured event for each matched downstream request.
// Router-generated 404 and 405 responses do not pass through route middleware.
func AccessLogger(opts ...AccessLogOption) Middleware {
	config := accessLogConfig{logger: slog.Default()}
	for _, opt := range opts {
		opt(&config)
	}
	if config.logger == nil {
		config.logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &responseStateWriter{ResponseWriter: w}
			started := time.Now()
			next.ServeHTTP(state, r)
			attrs := requestAttrs(r)
			attrs = append(attrs,
				slog.Int("status", state.Status()),
				slog.Float64("duration_seconds", time.Since(started).Seconds()),
			)
			observabilitysafe.Call(func() {
				config.logger.LogAttrs(r.Context(), slog.LevelInfo, EventAccessLog, attrs...)
			})
		})
	}
}

func instrumentHTTP(next http.Handler, recorder observability.Recorder, method, route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &responseStateWriter{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(state, r)
		observabilitysafe.Call(func() {
			recorder.ObserveHistogram(observability.MetricHTTPRequestDuration, time.Since(started).Seconds(),
				slog.String("route", route), slog.String("method", method), slog.Int("status", state.Status()))
		})
	})
}

type responseStateWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseStateWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStateWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseStateWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseStateWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func requestAttrs(r *http.Request) []slog.Attr {
	attrs := []slog.Attr{slog.String("route", requestRoutePattern(r)), slog.String("method", r.Method)}
	if requestID, ok := RequestIDFromContext(r.Context()); ok {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	return attrs
}

func requestRoutePattern(r *http.Request) string {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		return routeContext.RoutePattern()
	}
	return ""
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r > unicode.MaxASCII || unicode.IsSpace(r) || unicode.IsControl(r) || r == ',' {
			return false
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	const separators = "()<>@,;:\\\"/[]?={} \t"
	for _, r := range name {
		if r > unicode.MaxASCII || r <= 31 || r == 127 || strings.ContainsRune(separators, r) {
			return false
		}
	}
	return true
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

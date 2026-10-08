package tango_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/ratelimit"
)

// scopeLog is one structured log record: its message and attributes.
type scopeLog struct {
	message string
	attrs   map[string]any
}

// scopeLogs is an slog.Handler keeping every record.
type scopeLogs struct {
	mu      *sync.Mutex
	records *[]scopeLog
	attrs   []slog.Attr
}

func newScopeLogger() (*slog.Logger, *scopeLogs) {
	h := &scopeLogs{mu: &sync.Mutex{}, records: &[]scopeLog{}}
	return slog.New(h), h
}

func (h *scopeLogs) Enabled(context.Context, slog.Level) bool { return true }
func (h *scopeLogs) WithGroup(string) slog.Handler            { return h }
func (h *scopeLogs) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}
func (h *scopeLogs) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]any{}
	for _, a := range h.attrs {
		attrs[a.Key] = a.Value.Any()
	}
	record.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.Any(); return true })
	h.mu.Lock()
	*h.records = append(*h.records, scopeLog{record.Message, attrs})
	h.mu.Unlock()
	return nil
}

func (h *scopeLogs) byMessage(message string) []scopeLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []scopeLog
	for _, r := range *h.records {
		if r.message == message {
			out = append(out, r)
		}
	}
	return out
}

// scopeMetrics is an observability.Recorder keeping every histogram
// observation's attributes.
type scopeMetrics struct {
	mu           sync.Mutex
	observations []map[string]any
}

func (m *scopeMetrics) AddCounter(string, int64, ...slog.Attr) {}
func (m *scopeMetrics) ObserveHistogram(_ string, _ float64, attrs ...slog.Attr) {
	values := map[string]any{}
	for _, a := range attrs {
		values[a.Key] = a.Value.Any()
	}
	m.mu.Lock()
	m.observations = append(m.observations, values)
	m.mu.Unlock()
}

// counting is global middleware counting the requests it sees.
type counting struct {
	mu sync.Mutex
	n  int
}

func (c *counting) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// observedSite serves routes under /items/ with config, adding global's
// middleware, observed with a capturing logger and recorder.
func observedSite(t *testing.T, config tango.Config, routes tango.URLs, global func(*slog.Logger) []tango.Middleware) (http.Handler, *scopeLogs, *scopeMetrics) {
	t.Helper()
	logger, logs := newScopeLogger()
	metrics := &scopeMetrics{}
	config.InstalledApps = []tango.App{tango.NewApp("items", func(r *tango.Registry) error {
		return r.Routes().Include("/items/", routes)
	})}
	if global != nil {
		config.Middleware = global(logger)
	}
	registry, err := tango.BuildRegistry(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	tango.ObserveRoutes(registry, logger, metrics)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, logs, metrics
}

// scopeSite is an app with GET /items/{id}/ and POST /items/, served with
// scope and global.
func scopeSite(t *testing.T, scope tango.MiddlewareScope, global func(logger *slog.Logger) []tango.Middleware) (http.Handler, *scopeLogs, *scopeMetrics) {
	t.Helper()
	return observedSite(t, tango.Config{MiddlewareScope: scope}, tango.URLs{
		tango.Path(http.MethodGet, "/{id}/", func(ctx *tango.Context) error {
			ctx.Logger().Info("in view")
			return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Param("id")})
		}),
		tango.Path(http.MethodPost, "/", func(ctx *tango.Context) error {
			var v map[string]any
			if err := ctx.Bind(&v); err != nil {
				return err
			}
			return ctx.JSON(http.StatusCreated, v)
		}),
	}, global)
}

// observedMiddleware is RequestID, Recoverer and AccessLogger logging to
// logger, followed by extra.
func observedMiddleware(logger *slog.Logger, extra ...tango.Middleware) []tango.Middleware {
	return append([]tango.Middleware{
		tango.RequestID(tango.WithRequestIDLogger(logger)),
		tango.Recoverer(tango.WithRecoveryLogger(logger)),
		tango.AccessLogger(tango.WithAccessLogger(logger)),
	}, extra...)
}

func serve(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, target, strings.NewReader(body)))
	return response
}

func TestScopeAllObservesUnmatchedRequests(t *testing.T) {
	tests := []struct {
		name, method, target string
		status               int
	}{
		{"not found", http.MethodGet, "/nowhere/", http.StatusNotFound},
		{"method not allowed", http.MethodDelete, "/items/42/", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, logs, metrics := scopeSite(t, tango.MiddlewareScopeAll, func(l *slog.Logger) []tango.Middleware { return observedMiddleware(l) })
			response := serve(handler, tt.method, tt.target, "")
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("no X-Request-ID: RequestID didn't run")
			}
			access := logs.byMessage(tango.EventAccessLog)
			if len(access) != 1 || access[0].attrs["route"] != "(unmatched)" || access[0].attrs["status"] != int64(tt.status) {
				t.Fatalf("access logs = %+v, want one with route (unmatched), status %d", access, tt.status)
			}
			if len(metrics.observations) != 1 || metrics.observations[0]["route"] != "(unmatched)" || metrics.observations[0]["status"] != int64(tt.status) {
				t.Fatalf("metrics = %+v, want one with route (unmatched), status %d", metrics.observations, tt.status)
			}
		})
	}
}

func TestScopeAllRejectsAnOversizedBodyToAnUnmatchedPath(t *testing.T) {
	handler, logs, metrics := scopeSite(t, tango.MiddlewareScopeAll, func(l *slog.Logger) []tango.Middleware {
		return observedMiddleware(l, tango.MaxBodySize(8))
	})
	if response := serve(handler, http.MethodPost, "/nowhere/", `{"far":"too long for eight bytes"}`); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.Code)
	}
	access := logs.byMessage(tango.EventAccessLog)
	if len(access) != 1 || access[0].attrs["route"] != "(unmatched)" || access[0].attrs["status"] != int64(http.StatusRequestEntityTooLarge) {
		t.Fatalf("access logs = %+v, want one with route (unmatched), status 413", access)
	}
	if len(metrics.observations) != 1 || metrics.observations[0]["route"] != "(unmatched)" {
		t.Fatalf("metrics = %+v, want one with route (unmatched)", metrics.observations)
	}
}

func TestScopeAllRecoversAPanicOnAnUnmatchedRequest(t *testing.T) {
	panicking := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	}
	handler, logs, _ := scopeSite(t, tango.MiddlewareScopeAll, func(l *slog.Logger) []tango.Middleware { return observedMiddleware(l, panicking) })
	if response := serve(handler, http.MethodGet, "/nowhere/", ""); response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want Recoverer's 500", response.Code)
	}
	if panics := logs.byMessage(tango.EventPanic); len(panics) != 1 || panics[0].attrs["route"] != "(unmatched)" {
		t.Fatalf("panic logs = %+v, want one with route (unmatched)", panics)
	}
}

// TestScopeAllReportsTheRealRouteForGlobalShortCircuits: global middleware
// answering before routing still reports the matched route.
func TestScopeAllReportsTheRealRouteForGlobalShortCircuits(t *testing.T) {
	limiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 1, Refill: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, target, body, route string
		extra                             tango.Middleware
		status                            int
	}{
		{"ratelimit 429", http.MethodGet, "/items/42/", "", "/items/{id}/", ratelimit.Middleware(limiter, ratelimit.RemoteIPKey()), http.StatusTooManyRequests},
		{"MaxBodySize early 413", http.MethodPost, "/items/", `{"far":"too long for eight bytes"}`, "/items/", tango.MaxBodySize(8), http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, logs, metrics := scopeSite(t, tango.MiddlewareScopeAll, func(l *slog.Logger) []tango.Middleware { return observedMiddleware(l, tt.extra) })
			var last *httptest.ResponseRecorder
			for i := 0; i < 2; i++ {
				last = serve(handler, tt.method, tt.target, tt.body)
			}
			if last.Code != tt.status {
				t.Fatalf("status = %d, want %d", last.Code, tt.status)
			}
			access := logs.byMessage(tango.EventAccessLog)
			metrics.mu.Lock()
			defer metrics.mu.Unlock()
			for i, a := range access {
				if a.attrs["route"] != tt.route || metrics.observations[i]["route"] != tt.route {
					t.Fatalf("request %d reported route %v / %v, want %q", i, a.attrs["route"], metrics.observations[i]["route"], tt.route)
				}
			}
		})
	}
}

// TestAMatchedRouteIsObservedTheSameUnderBothScopes compares every log and
// metric for one matched request, minus the per-request values.
func TestAMatchedRouteIsObservedTheSameUnderBothScopes(t *testing.T) {
	observe := func(scope tango.MiddlewareScope) ([]scopeLog, []map[string]any) {
		handler, logs, metrics := scopeSite(t, scope, func(l *slog.Logger) []tango.Middleware { return observedMiddleware(l) })
		serve(handler, http.MethodGet, "/items/42/", "")
		for _, r := range *logs.records {
			delete(r.attrs, "request_id")
			delete(r.attrs, "duration_seconds")
		}
		return *logs.records, metrics.observations
	}
	routesLogs, routesMetrics := observe(tango.MiddlewareScopeRoutes)
	allLogs, allMetrics := observe(tango.MiddlewareScopeAll)
	if !equalLogs(routesLogs, allLogs) {
		t.Fatalf("logs differ:\nRoutes: %+v\nAll:    %+v", routesLogs, allLogs)
	}
	if len(routesMetrics) != 1 || len(allMetrics) != 1 || !equalAttrs(routesMetrics[0], allMetrics[0]) {
		t.Fatalf("metrics differ:\nRoutes: %+v\nAll:    %+v", routesMetrics, allMetrics)
	}
}

func equalLogs(a, b []scopeLog) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].message != b[i].message || !equalAttrs(a[i].attrs, b[i].attrs) {
			return false
		}
	}
	return true
}

func equalAttrs(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestGlobalMiddlewareRunsOncePerRequest(t *testing.T) {
	for _, target := range []string{"/items/42/", "/nowhere/"} {
		t.Run(target, func(t *testing.T) {
			c := &counting{}
			handler, _, _ := scopeSite(t, tango.MiddlewareScopeAll, func(*slog.Logger) []tango.Middleware { return []tango.Middleware{c.middleware} })
			serve(handler, http.MethodGet, target, "")
			if c.n != 1 {
				t.Fatalf("global middleware ran %d times, want once", c.n)
			}
		})
	}
}

func TestDefaultAndRoutesScopesLeaveUnmatchedRequestsUnobserved(t *testing.T) {
	for _, scope := range []tango.MiddlewareScope{tango.MiddlewareScopeDefault, tango.MiddlewareScopeRoutes} {
		c := &counting{}
		handler, _, metrics := scopeSite(t, scope, func(*slog.Logger) []tango.Middleware { return []tango.Middleware{c.middleware} })
		if response := serve(handler, http.MethodGet, "/nowhere/", ""); response.Code != http.StatusNotFound {
			t.Fatalf("scope %d: status = %d, want 404", scope, response.Code)
		}
		if c.n != 0 || len(metrics.observations) != 0 {
			t.Fatalf("scope %d: global middleware ran %d times, metrics %+v; want neither", scope, c.n, metrics.observations)
		}
		serve(handler, http.MethodGet, "/items/42/", "")
		if c.n != 1 {
			t.Fatalf("scope %d: global middleware ran %d times for a matched route, want once", scope, c.n)
		}
	}
}

func TestBuildRegistryRejectsAnUnknownMiddlewareScope(t *testing.T) {
	_, err := tango.BuildRegistry(tango.Config{MiddlewareScope: tango.MiddlewareScope(99)})
	if err == nil || !strings.Contains(err.Error(), "MiddlewareScope") {
		t.Fatalf("error = %v, want one naming MiddlewareScope", err)
	}
}

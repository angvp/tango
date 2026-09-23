package tango

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/observability"
)

type capturedLog struct {
	message string
	attrs   map[string]any
}

type capturingHandler struct {
	mu      *sync.Mutex
	records *[]capturedLog
	attrs   []slog.Attr
	panic   bool
}

func newCapturingLogger() (*slog.Logger, *[]capturedLog) {
	records := []capturedLog{}
	return slog.New(&capturingHandler{mu: &sync.Mutex{}, records: &records}), &records
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) WithGroup(string) slog.Handler            { return h }
func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}
func (h *capturingHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any)
	for _, attr := range h.attrs {
		attrs[attr.Key] = attr.Value.Any()
	}
	record.Attrs(func(attr slog.Attr) bool { attrs[attr.Key] = attr.Value.Any(); return true })
	h.mu.Lock()
	*h.records = append(*h.records, capturedLog{message: record.Message, attrs: attrs})
	h.mu.Unlock()
	if h.panic {
		panic("handler panic")
	}
	return nil
}

type metricCall struct {
	name  string
	value float64
	attrs map[string]any
}

type capturingRecorder struct {
	mu         sync.Mutex
	counters   []metricCall
	histograms []metricCall
	panic      bool
}

func attrsMap(attrs []slog.Attr) map[string]any {
	values := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		values[attr.Key] = attr.Value.Any()
	}
	return values
}

func (r *capturingRecorder) AddCounter(name string, delta int64, attrs ...slog.Attr) {
	r.mu.Lock()
	r.counters = append(r.counters, metricCall{name: name, value: float64(delta), attrs: attrsMap(attrs)})
	r.mu.Unlock()
	if r.panic {
		panic("recorder panic")
	}
}
func (r *capturingRecorder) ObserveHistogram(name string, value float64, attrs ...slog.Attr) {
	r.mu.Lock()
	r.histograms = append(r.histograms, metricCall{name: name, value: value, attrs: attrsMap(attrs)})
	r.mu.Unlock()
	if r.panic {
		panic("recorder panic")
	}
}

func buildObservedHandler(t *testing.T, view View, logger *slog.Logger, recorder observability.Recorder, middleware ...Middleware) http.Handler {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(NewApp("observed", func(r *Registry) error {
		return r.Routes().Include("/items/", URLs{Path(http.MethodGet, "/{id}/", view, Use(middleware...))})
	})); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	registry.Routes().setObservability(logger, recorder, recorder != nil)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestStructuredViewErrorUsesRoutePatternAndRequestContext(t *testing.T) {
	logger, records := newCapturingLogger()
	handler := buildObservedHandler(t, func(ctx *Context) error {
		if ctx.Logger() == nil {
			t.Fatal("Context.Logger() is nil")
		}
		return errors.New("boom")
	}, logger, nil, RequestID())

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/items/42/?secret=x", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if len(*records) != 1 || (*records)[0].message != EventViewError {
		t.Fatalf("records = %+v", *records)
	}
	attrs := (*records)[0].attrs
	if attrs["route"] != "/items/{id}/" || attrs["method"] != http.MethodGet || attrs["request_id"] == "" {
		t.Fatalf("attrs = %+v", attrs)
	}
}

func TestRecovererContainsPanickingLoggerAndOriginalPanic(t *testing.T) {
	records := []capturedLog{}
	logger := slog.New(&capturingHandler{mu: &sync.Mutex{}, records: &records, panic: true})
	handler := Recoverer(WithRecoveryLogger(logger))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("application panic")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError || len(records) != 1 || records[0].message != EventPanic {
		t.Fatalf("status=%d records=%+v", recorder.Code, records)
	}
}

func TestRequestIDTrustedHeaderAndGenerationFailure(t *testing.T) {
	t.Run("trusted", func(t *testing.T) {
		handler := RequestID(WithTrustedHeader("X-Gateway-Request"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, ok := RequestIDFromContext(r.Context()); !ok || got != "gateway-123" {
				t.Fatalf("request id = %q, %v", got, ok)
			}
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("X-Gateway-Request", "gateway-123")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Header().Get("X-Request-ID") != "gateway-123" {
			t.Fatalf("header = %q", response.Header().Get("X-Request-ID"))
		}
	})

	t.Run("generation failure", func(t *testing.T) {
		original := generateRequestID
		generateRequestID = func() (string, error) { return "", errors.New("entropy unavailable") }
		t.Cleanup(func() { generateRequestID = original })
		logger, records := newCapturingLogger()
		called := false
		handler := RequestID(WithRequestIDLogger(logger))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if !called || response.Header().Get("X-Request-ID") != "" || len(*records) != 1 || (*records)[0].message != EventRequestIDGenerationFailed {
			t.Fatalf("called=%v header=%q records=%+v", called, response.Header().Get("X-Request-ID"), *records)
		}
	})

	t.Run("invalid trusted values generate fresh IDs", func(t *testing.T) {
		for _, value := range []string{"", "contains space", "contains,comma", "contains\nnewline", strings.Repeat("x", 129), "contains/slash"} {
			t.Run(value, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, "/", nil)
				request.Header.Set("X-Gateway-Request", value)
				response := httptest.NewRecorder()
				RequestID(WithTrustedHeader("X-Gateway-Request"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestID, ok := RequestIDFromContext(r.Context())
					if !ok || requestID == "" || requestID == value {
						t.Fatalf("request ID = %q, ok = %v", requestID, ok)
					}
				})).ServeHTTP(response, request)
			})
		}
	})
}

func TestWithTrustedHeaderRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "bad header", "bad\nheader", "bad:header", "bad,header"} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			_ = WithTrustedHeader(name)
		})
	}
}

func TestAutomaticHTTPMetricCoversMiddlewareShortCircuitAndContainsRecorderPanic(t *testing.T) {
	recorder := &capturingRecorder{panic: true}
	shortCircuit := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	})
	handler := buildObservedHandler(t, func(ctx *Context) error {
		t.Fatal("view must not run")
		return nil
	}, slog.Default(), recorder, shortCircuit)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/99/", nil))
	if response.Code != http.StatusUnauthorized || len(recorder.histograms) != 1 {
		t.Fatalf("status=%d metrics=%+v", response.Code, recorder.histograms)
	}
	metric := recorder.histograms[0]
	if metric.name != observability.MetricHTTPRequestDuration || metric.attrs["route"] != "/items/{id}/" || metric.attrs["status"] != int64(http.StatusUnauthorized) || metric.value < 0 || metric.value > time.Second.Seconds() {
		t.Fatalf("metric = %+v", metric)
	}
}

func TestAutomaticHTTPMetricCoversSuccessAndViewError(t *testing.T) {
	for _, test := range []struct {
		name       string
		view       View
		wantStatus int64
	}{
		{name: "success", view: func(ctx *Context) error { return ctx.JSON(http.StatusCreated, map[string]bool{"ok": true}) }, wantStatus: http.StatusCreated},
		{name: "view error", view: func(*Context) error { return errors.New("boom") }, wantStatus: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := &capturingRecorder{}
			handler := buildObservedHandler(t, test.view, slog.Default(), recorder)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/42/?secret=true", nil))
			if len(recorder.histograms) != 1 {
				t.Fatalf("metrics = %+v", recorder.histograms)
			}
			metric := recorder.histograms[0]
			if metric.attrs["route"] != "/items/{id}/" || metric.attrs["method"] != http.MethodGet || metric.attrs["status"] != test.wantStatus {
				t.Fatalf("metric = %+v", metric)
			}
		})
	}
}

func TestAccessLoggerCapturesImplicitStatus(t *testing.T) {
	logger, records := newCapturingLogger()
	handler := AccessLogger(WithAccessLogger(logger))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/hello?secret=true", nil))
	if len(*records) != 1 || (*records)[0].message != EventAccessLog || (*records)[0].attrs["status"] != int64(http.StatusOK) {
		t.Fatalf("records = %+v", *records)
	}
}

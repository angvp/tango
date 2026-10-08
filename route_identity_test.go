package tango

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEveryLayerReportsTheSameRoute pins one route identity per request:
// Context.Logger, the View-error log, RequestID/Recoverer/AccessLogger and
// automatic metrics all report the matched route's pattern, never the URL.
func TestEveryLayerReportsTheSameRoute(t *testing.T) {
	const want = "/items/{id}/"
	tests := []struct {
		name   string
		view   View
		events []string
	}{
		{"View error", func(ctx *Context) error {
			ctx.Logger().Info("in view")
			return errors.New("boom")
		}, []string{"in view", EventViewError, EventAccessLog}},
		{"panic", func(ctx *Context) error { panic("boom") }, []string{EventPanic, EventAccessLog}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, records := newCapturingLogger()
			recorder := &capturingRecorder{}
			handler := buildObservedHandler(t, tt.view, logger, recorder,
				RequestID(WithRequestIDLogger(logger)), AccessLogger(WithAccessLogger(logger)), Recoverer(WithRecoveryLogger(logger)))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/42/?q=x", nil))

			seen := map[string]bool{}
			for _, record := range *records {
				if got := record.attrs["route"]; got != want {
					t.Fatalf("%s logged route %v, want %q", record.message, got, want)
				}
				seen[record.message] = true
			}
			for _, event := range tt.events {
				if !seen[event] {
					t.Fatalf("no %s log; got %+v", event, *records)
				}
			}
			if len(recorder.histograms) != 1 || recorder.histograms[0].attrs["route"] != want {
				t.Fatalf("metrics = %+v, want one observation with route %q", recorder.histograms, want)
			}
		})
	}
}

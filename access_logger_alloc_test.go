package tango

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A request whose access event the logger would discard costs nothing beyond
// the wrapped handler itself.
func TestAccessLoggerAddsNoAllocationsWhenInfoIsDisabled(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	logged := AccessLogger(WithAccessLogger(slog.New(&levelProbe{level: slog.LevelWarn})))(inner)
	request := httptest.NewRequest(http.MethodGet, "/hello", nil)
	run := func(h http.Handler) float64 {
		return testing.AllocsPerRun(100, func() { h.ServeHTTP(httptest.NewRecorder(), request) })
	}
	bare, wrapped := run(inner), run(logged)
	if wrapped > bare {
		t.Fatalf("AccessLogger added %.0f allocations per request with Info disabled; want none", wrapped-bare)
	}
}

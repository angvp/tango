package tango

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// levelProbe is a slog.Handler that records how it was used.
type levelProbe struct {
	level          slog.Level
	enabledCalls   int
	handled        int
	panicOnEnabled bool
}

func (p *levelProbe) Enabled(_ context.Context, level slog.Level) bool {
	p.enabledCalls++
	if p.panicOnEnabled {
		panic("broken handler")
	}
	return level >= p.level
}

func (p *levelProbe) Handle(context.Context, slog.Record) error { p.handled++; return nil }
func (p *levelProbe) WithAttrs([]slog.Attr) slog.Handler        { return p }
func (p *levelProbe) WithGroup(string) slog.Handler             { return p }

func serveOnce(t *testing.T, probe *levelProbe) *httptest.ResponseRecorder {
	t.Helper()
	handler := AccessLogger(WithAccessLogger(slog.New(probe)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/hello", nil))
	return response
}

func TestAccessLoggerDoesNothingWhenInfoIsDisabled(t *testing.T) {
	probe := &levelProbe{level: slog.LevelWarn}
	response := serveOnce(t, probe)
	if response.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the handler's own", response.Code)
	}
	if probe.enabledCalls != 1 || probe.handled != 0 {
		t.Fatalf("enabled calls = %d, handled = %d; want one level check and no event", probe.enabledCalls, probe.handled)
	}
}

func TestAccessLoggerLogsWhenInfoIsEnabled(t *testing.T) {
	probe := &levelProbe{level: slog.LevelInfo}
	serveOnce(t, probe)
	if probe.handled != 1 {
		t.Fatalf("handled = %d, want one access event", probe.handled)
	}
}

func TestAccessLoggerSurvivesAHandlerThatPanicsOnEnabled(t *testing.T) {
	probe := &levelProbe{level: slog.LevelInfo, panicOnEnabled: true}
	response := serveOnce(t, probe)
	if response.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the handler's own", response.Code)
	}
}

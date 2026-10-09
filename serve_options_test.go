package tango

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/observability"
	"github.com/angvp/tango/testdb"
)

// lockedBuffer is a bytes.Buffer the server and the test can use together.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serveRoutes starts ServeContext with an "ok" route and a "boom" route whose
// view fails, and returns the base URL.
func serveRoutes(t *testing.T, opts ...ServeOption) string {
	t.Helper()
	sqlDB := openTestDB(t)
	addrCh := captureListenerAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	app := NewApp("routes", func(r *Registry) error {
		return r.Routes().Include("/", URLs{
			Path(http.MethodGet, "/ok/", func(ctx *Context) error { return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"}) }),
			Path(http.MethodGet, "/boom/", func(*Context) error { return errors.New("the view failed") }),
		})
	})
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{app}}, sqlDB, testdb.Dialect(), opts...)
	}()
	select {
	case addr := <-addrCh:
		// Stop the server and wait for it, once it is known to be running.
		t.Cleanup(func() { cancel(); <-done })
		return "http://" + addr
	case err := <-done:
		cancel()
		t.Fatalf("ServeContext returned before listening: %v", err)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("ServeContext never listened")
	}
	return ""
}

func fetch(t *testing.T, url string) int {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func TestWithLoggerReceivesTheFrameworksLogs(t *testing.T) {
	var logs lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	base := serveRoutes(t, WithLogger(logger))

	if status := fetch(t, base+"/boom/"); status != http.StatusInternalServerError {
		t.Fatalf("GET /boom/ = %d, want 500", status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), EventViewError) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := logs.String()
	if !strings.Contains(got, EventViewError) || !strings.Contains(got, "the view failed") {
		t.Fatalf("the configured logger saw %q, want the view error and its message", got)
	}
}

func TestWithRecorderReceivesTheRequestMetrics(t *testing.T) {
	recorder := &capturingRecorder{}
	base := serveRoutes(t, WithRecorder(recorder))

	if status := fetch(t, base+"/ok/"); status != http.StatusOK {
		t.Fatalf("GET /ok/ = %d", status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recorder.mu.Lock()
		n := len(recorder.histograms)
		recorder.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	found := false
	for _, metric := range recorder.histograms {
		if metric.name == observability.MetricHTTPRequestDuration && metric.attrs["route"] == "/ok/" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the recorder saw %+v, want the request duration of /ok/", recorder.histograms)
	}
}

func TestWithRecorderTreatsNilAndNopAsNoRecording(t *testing.T) {
	recorder := &capturingRecorder{}
	tests := []struct {
		name        string
		option      ServeOption
		wantEnabled bool
	}{
		{"nil", WithRecorder(nil), false},
		{"the no-op recorder", WithRecorder(observability.NopRecorder{}), false},
		{"a real recorder", WithRecorder(recorder), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := newServeConfig(tt.option)
			if sc.recorderEnabled != tt.wantEnabled {
				t.Errorf("recorderEnabled = %v, want %v", sc.recorderEnabled, tt.wantEnabled)
			}
			if sc.recorder == nil {
				t.Error("recorder is nil, want it normalised to a usable one")
			}
		})
	}
}

func TestWithLoggerNilIsRefusedBeforeListening(t *testing.T) {
	addrCh := captureListenerAddr(t)
	err := ServeContext(context.Background(), Config{Addr: "127.0.0.1:0"}, openTestDB(t), testdb.Dialect(), WithLogger(nil))
	if err == nil || !strings.Contains(err.Error(), "logger must not be nil") {
		t.Fatalf("error = %v, want one saying the logger must not be nil", err)
	}
	select {
	case addr := <-addrCh:
		t.Fatalf("ServeContext listened on %s despite the nil logger", addr)
	default:
	}
}

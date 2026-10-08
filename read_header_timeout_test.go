package tango

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/testdb"
)

// serveForHeaderTimeout starts ServeContext with opts and an "ok" route,
// and returns the address it listens on.
func serveForHeaderTimeout(t *testing.T, opts ...ServeOption) string {
	t.Helper()
	sqlDB := openTestDB(t)
	addrCh := captureListenerAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	app := NewApp("ok", func(r *Registry) error {
		return r.Routes().Include("/", URLs{Path(http.MethodGet, "/", func(ctx *Context) error {
			return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"})
		})})
	})
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{app}}, sqlDB, testdb.Dialect(), opts...)
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case addr := <-addrCh:
		return addr
	case err := <-done:
		t.Fatalf("ServeContext returned before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("ServeContext never listened")
	}
	return ""
}

// requestHeadersSlowly sends a request's headers, pausing for pause
// before the final blank line, and returns the response's status line, or
// "" if the server closed the connection without answering.
func requestHeadersSlowly(t *testing.T, addr string, pause time.Duration) string {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: test\r\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(pause)
	_, _ = conn.Write([]byte("\r\n"))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, _ := bufio.NewReader(conn).ReadString('\n')
	return strings.TrimSpace(status)
}

func TestReadHeaderTimeoutDropsASlowClientAndServesAPromptOne(t *testing.T) {
	addr := serveForHeaderTimeout(t, WithReadHeaderTimeout(200*time.Millisecond))
	if status := requestHeadersSlowly(t, addr, 600*time.Millisecond); strings.HasSuffix(status, "200 OK") {
		t.Fatalf("a client slower than the header timeout was served: %q", status)
	}
	if status := requestHeadersSlowly(t, addr, 0); !strings.HasSuffix(status, "200 OK") {
		t.Fatalf("a prompt client got %q, want 200 OK", status)
	}
}

func TestReadHeaderTimeoutDefaultsToTenSecondsAndZeroDisablesIt(t *testing.T) {
	tests := []struct {
		name string
		opts []ServeOption
		want time.Duration
	}{
		{"omitted", nil, 10 * time.Second},
		{"zero", []ServeOption{WithReadHeaderTimeout(0)}, 0},
		{"set", []ServeOption{WithReadHeaderTimeout(3 * time.Second)}, 3 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := newServeConfig(tt.opts...)
			if got := newHTTPServer(":0", http.NotFoundHandler(), sc).ReadHeaderTimeout; got != tt.want {
				t.Fatalf("ReadHeaderTimeout = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestReadHeaderTimeoutRejectsANegativeDurationBeforeListening(t *testing.T) {
	addrCh := captureListenerAddr(t)
	err := ServeContext(context.Background(), Config{Addr: "127.0.0.1:0"}, openTestDB(t), testdb.Dialect(), WithReadHeaderTimeout(-time.Second))
	if err == nil || !strings.Contains(err.Error(), "read header timeout") {
		t.Fatalf("error = %v, want one naming the read header timeout", err)
	}
	select {
	case addr := <-addrCh:
		t.Fatalf("ServeContext listened on %s despite the invalid timeout", addr)
	default:
	}
}

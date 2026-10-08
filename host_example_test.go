package tango_test

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reusableHostDir is the Host project the reusable-app tests run.
const reusableHostDir = "examples/reusable-greetings-host"

// hostExample is a built copy of the reusable-greetings host with a
// database of its own, so no test shares state with another test, another
// run, or the source tree.
type hostExample struct {
	binary string
	dbPath string
}

// buildHostExample builds the host into the test's temporary directory.
// Running the built binary, rather than `go run`, means stopping it stops
// the server itself: killing `go run` leaves its child running and
// listening.
func buildHostExample(t *testing.T) hostExample {
	t.Helper()
	dir := t.TempDir()
	host := hostExample{binary: filepath.Join(dir, "host"), dbPath: filepath.Join(dir, "app.db")}
	build := exec.Command("go", "build", "-o", host.binary, ".")
	build.Dir = reusableHostDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build in %s failed: %v\n%s", reusableHostDir, err, out)
	}
	return host
}

// command runs the host with args against its own database, listening on
// addr when it serves.
func (h hostExample) command(addr string, args ...string) *exec.Cmd {
	cmd := exec.Command(h.binary, args...)
	cmd.Env = append(os.Environ(), "TANGO_DB_DSN=sqlite://"+h.dbPath, "TANGO_ADDR="+addr)
	return cmd
}

// run runs the host with args to completion, failing the test if it fails.
func (h hostExample) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := h.command("", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("host %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// serve starts the host's server on a free local port, waits until it
// answers, and returns its base URL. The server is stopped when the test
// ends.
func (h hostExample) serve(t *testing.T) string {
	t.Helper()
	addr := freeLocalAddr(t)
	cmd := h.command(addr)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start host server: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})

	baseURL := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			t.Fatalf("host server exited before answering:\n%s", out.String())
		default:
		}
		if resp, err := http.Get(baseURL + "/"); err == nil {
			resp.Body.Close()
			return baseURL
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Stop it first: its output is written until it exits.
	_ = cmd.Process.Kill()
	<-exited
	t.Fatalf("host server never answered on %s:\n%s", addr, out.String())
	return ""
}

// freeLocalAddr returns a loopback address with a port nothing is
// listening on.
func freeLocalAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

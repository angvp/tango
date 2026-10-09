package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// localTangoRunner runs commands for real, pointing a freshly initialized
// module at this checkout of tanGO so `go mod tidy` resolves it here
// instead of fetching a release.
type localTangoRunner struct{ repo string }

func (r localTangoRunner) Run(ctx context.Context, dir, name string, args []string, stdout, stderr io.Writer) error {
	if err := (ExecRunner{}).Run(ctx, dir, name, args, stdout, stderr); err != nil {
		return err
	}
	if name == "go" && len(args) > 1 && args[0] == "mod" && args[1] == "init" {
		return (ExecRunner{}).Run(ctx, dir, "go", []string{"mod", "edit", "-replace", "github.com/angvp/tango=" + r.repo}, stdout, stderr)
	}
	return nil
}

// lockedBuffer is a bytes.Buffer safe to write from a running process
// while the test reads it.
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

// TestANewProjectRunsSafelyAsGenerated builds a generated project against
// this checkout and runs it as a hosting platform would: only PORT set.
func TestANewProjectRunsSafelyAsGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated project")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var stderr strings.Builder
	if code := Run(context.Background(), []string{"newproject", "shop"}, dir, io.Discard, &stderr, localTangoRunner{repo: repo}); code != 0 {
		t.Fatalf("newproject exit code = %d: %s", code, stderr.String())
	}
	project := filepath.Join(dir, "shop")
	binary := filepath.Join(dir, "shop-bin")
	build := goBuild("-o", binary, ".")
	build.Dir = project
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TANGO_DB_DSN=sqlite://" + filepath.Join(dir, "app.db")}
	env = withChildCover(env)
	check := exec.Command(binary, "-check")
	check.Dir, check.Env = project, env
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("-check: %v\n%s", err, out)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	listener.Close()
	server := exec.Command(binary)
	var output lockedBuffer
	server.Dir, server.Env, server.Stdout, server.Stderr = project, append(env, "PORT="+port), &output, &output
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })

	base := "http://127.0.0.1:" + port
	deadline := time.Now().Add(20 * time.Second)
	for {
		if resp, err := http.Get(base + "/nowhere/"); err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("GET /nowhere/ = %d, want 404", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the project never listened on PORT %s:\n%s", port, output.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The scaffold's limit is 1 MiB, whether or not the client declares
	// the body's length up front.
	bodies := []struct {
		name     string
		body     io.Reader
		tooLarge bool
	}{
		{"declared, over 1 MiB", strings.NewReader(strings.Repeat("x", 1<<20+1)), true},
		// An io.MultiReader's length is unknown, so the client sends the
		// body chunked and only the limited reader can catch it.
		{"chunked, over 1 MiB", io.MultiReader(strings.NewReader(strings.Repeat("x", 1<<20+1))), true},
		{"declared, under 1 MiB", strings.NewReader(strings.Repeat("x", 1<<20-1024)), false},
	}
	for _, tt := range bodies {
		request, err := http.NewRequest(http.MethodPost, base+"/admin/login/", tt.body)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		resp.Body.Close()
		if got := resp.StatusCode == http.StatusRequestEntityTooLarge; got != tt.tooLarge {
			t.Fatalf("%s: POST /admin/login/ = %d, want 413: %v", tt.name, resp.StatusCode, tt.tooLarge)
		}
	}

	logged := func() bool {
		out := output.String()
		return strings.Contains(out, "tango.http.access") && strings.Contains(out, "route=(unmatched)") && strings.Contains(out, "status=404")
	}
	for wait := time.Now().Add(5 * time.Second); !logged() && time.Now().Before(wait); {
		time.Sleep(50 * time.Millisecond)
	}
	if !logged() {
		t.Fatalf("the 404 wasn't access-logged as route (unmatched):\n%s", output.String())
	}

	// A hosting platform stops the project with SIGTERM: it must drain and
	// exit cleanly, not be killed mid-request.
	if err := server.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- server.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("the project did not exit cleanly on SIGTERM: %v\n%s", err, output.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("the project ignored SIGTERM:\n%s", output.String())
	}
}

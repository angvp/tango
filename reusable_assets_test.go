package tango_test

// This file enforces Milestone 13's app-owned templates/static-assets
// convention: a reusable app serves its own embed.FS via an ordinary route,
// reachable once installed into a host project, with no framework-level
// asset-serving mechanism involved. See "App-owned templates and static
// assets" in docs/guides/reusable-apps.md and ticket 04 in
// .scratch/reusable-apps/issues.

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReusableAppServesOwnStaticAssetFromHost(t *testing.T) {
	const hostDir = "examples/reusable-greetings-host"
	dbPath := filepath.Join(hostDir, "app.db")
	_ = os.Remove(dbPath)
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = hostDir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start host server: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	const url = "http://localhost:8000/greetings/static/hello.txt"

	var body string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		b, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read response body: %v", readErr)
		}
		if resp.StatusCode == http.StatusOK {
			body = string(b)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if body == "" {
		t.Fatalf("never got a 200 from %s — the greetings app's embedded static asset is not reachable through the host", url)
	}
	const want = "Hello from the greetings reusable app's own embedded assets."
	if !strings.Contains(body, want) {
		t.Fatalf("static asset body = %q, want it to contain %q", body, want)
	}
}

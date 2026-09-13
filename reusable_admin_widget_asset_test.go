package tango_test

// This file enforces the asset-serving half of Milestone 16's reusable-app
// exit criterion: examples/reusable-greetings' admin.Widget for its Name
// field (widget.go, trimmedNameWidget) references its own JS asset, served
// via the exact same embed.FS + routes convention Milestone 13 already
// proved in reusable_assets_test.go. The widget's render/parse behavior
// itself is covered by a fast unit test inside the reusable app's own
// module (examples/reusable-greetings/greetings/widget_test.go); this test
// only proves the asset it references is actually reachable through a real
// running host, the same way reusable_assets_test.go already does for the
// app's other static asset.

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReusableAppWidgetAssetIsServedFromHost(t *testing.T) {
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

	const url = "http://localhost:8000/greetings/static/widget.js"

	var ok bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			ok = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !ok {
		t.Fatalf("never got a 200 from %s — the reusable app's contributed admin.Widget asset is not reachable through the host", url)
	}
}

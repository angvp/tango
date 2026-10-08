package tango_test

// This file enforces the app-owned templates/static-assets convention: a
// reusable app serves its own embed.FS via an ordinary route, reachable
// once installed into a host project, with no framework-level asset-serving
// mechanism involved. See "App-owned templates and static assets" in
// docs/guides/reusable-apps.md.

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReusableAppServesOwnStaticAssetFromHost(t *testing.T) {
	baseURL := buildHostExample(t).serve(t)
	url := baseURL + "/greetings/static/hello.txt"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200: the greetings app's embedded static asset is not reachable through the host", url, resp.StatusCode)
	}
	const want = "Hello from the greetings reusable app's own embedded assets."
	if body := string(b); !strings.Contains(body, want) {
		t.Fatalf("static asset body = %q, want it to contain %q", body, want)
	}
}

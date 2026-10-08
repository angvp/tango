package tango_test

// This file enforces the asset-serving half of a reusable app's admin
// widget: examples/reusable-greetings' admin.Widget for its Name
// field (widget.go, trimmedNameWidget) references its own JS asset, served
// via the exact same embed.FS + routes convention already proved in reusable_assets_test.go. The widget's render/parse behavior
// itself is covered by a fast unit test inside the reusable app's own
// module (examples/reusable-greetings/greetings/widget_test.go); this test
// only proves the asset it references is actually reachable through a real
// running host, the same way reusable_assets_test.go already does for the
// app's other static asset.

import (
	"io"
	"net/http"
	"testing"
)

func TestReusableAppWidgetAssetIsServedFromHost(t *testing.T) {
	baseURL := buildHostExample(t).serve(t)
	url := baseURL + "/greetings/static/widget.js"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200: the reusable app's contributed admin.Widget asset is not reachable through the host", url, resp.StatusCode)
	}
}

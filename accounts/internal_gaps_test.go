package accounts

import (
	"net/http"
	"testing"
)

// TestRateLimitKeyFallsBackToRawRemoteAddr covers rateLimitKey's error
// branch: a RemoteAddr without a port (net.SplitHostPort fails) falls back
// to the raw value rather than erroring or panicking, mirroring admin's
// own copy of this helper.
func TestRateLimitKeyFallsBackToRawRemoteAddr(t *testing.T) {
	r := &http.Request{RemoteAddr: "no-port-here"}
	if got := rateLimitKey(r); got != "no-port-here" {
		t.Fatalf("rateLimitKey = %q, want the raw RemoteAddr", got)
	}
}

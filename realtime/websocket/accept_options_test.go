package websocket

import (
	"context"
	"net/http/httptest"
	"testing"
)

// TestViewOriginOptionsComposeOnOneAcceptConfig covers ViewOptions applied in
// sequence to one View, where each option sees the AcceptOptions the previous
// one installed. Each row pairs an accepted origin with a rejected one so the
// expected handshake outcome is visible for both sides of the rule.
func TestViewOriginOptionsComposeOnOneAcceptConfig(t *testing.T) {
	cases := []struct {
		name       string
		opts       []ViewOption
		origin     string
		wantStatus int
	}{
		{
			name:       "no option keeps same-origin default and rejects cross origin",
			opts:       nil,
			origin:     "http://frontend.example",
			wantStatus: 403,
		},
		{
			name:       "empty pattern list adds no allowance",
			opts:       []ViewOption{WithOriginPatterns()},
			origin:     "http://frontend.example",
			wantStatus: 403,
		},
		{
			name:       "later pattern list replaces the earlier one",
			opts:       []ViewOption{WithOriginPatterns("first.example"), WithOriginPatterns("second.example")},
			origin:     "http://first.example",
			wantStatus: 403,
		},
		{
			name:       "later pattern list accepts its own origin",
			opts:       []ViewOption{WithOriginPatterns("first.example"), WithOriginPatterns("second.example")},
			origin:     "http://second.example",
			wantStatus: 0,
		},
		{
			name:       "insecure skip after patterns accepts an unlisted origin",
			opts:       []ViewOption{WithOriginPatterns("frontend.example"), WithInsecureSkipVerify()},
			origin:     "http://anything-at-all.example",
			wantStatus: 0,
		},
		{
			name:       "malformed pattern matches no origin",
			opts:       []ViewOption{WithOriginPatterns("[")},
			origin:     "http://frontend.example",
			wantStatus: 403,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeCoordinator{}
			server := httptest.NewServer(buildHandlerWithViewOptions(t, fc, alwaysAuth("alice"), fixedRoom("room-1"), tc.opts...))
			defer server.Close()

			conn, resp, err := dialWithOrigin(context.Background(), toWSURL(server.URL)+"/ws", tc.origin)
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("dial: %v (status %v), want accepted", err, resp)
				}
				defer conn.CloseNow()
				waitForCondition(t, func() bool { return fc.joinCallCount() == 1 }, "expected Join to have been called once")
				return
			}
			if err == nil {
				t.Fatal("handshake succeeded, want rejection")
			}
			if resp == nil || resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %v, want %d", resp, tc.wantStatus)
			}
			if got := fc.joinCallCount(); got != 0 {
				t.Fatalf("Join was called %d times for a rejected handshake, want 0", got)
			}
		})
	}
}

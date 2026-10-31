package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDensityFromRequestAcceptsOnlyTheExactValues(t *testing.T) {
	tests := []struct {
		name   string
		cookie *http.Cookie
		want   string
	}{
		{name: "no cookie is comfortable", cookie: nil, want: densityComfortable},
		{name: "empty value is comfortable", cookie: &http.Cookie{Name: densityCookieName, Value: ""}, want: densityComfortable},
		{name: "comfortable is comfortable", cookie: &http.Cookie{Name: densityCookieName, Value: "comfortable"}, want: densityComfortable},
		{name: "compact is compact", cookie: &http.Cookie{Name: densityCookieName, Value: "compact"}, want: densityCompact},
		{name: "upper case is not a valid value", cookie: &http.Cookie{Name: densityCookieName, Value: "COMPACT"}, want: densityComfortable},
		{name: "leading space is not a valid value", cookie: &http.Cookie{Name: densityCookieName, Value: " compact"}, want: densityComfortable},
		{name: "unknown word is comfortable", cookie: &http.Cookie{Name: densityCookieName, Value: "spacious"}, want: densityComfortable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
			if tt.cookie != nil {
				request.AddCookie(tt.cookie)
			}
			if got := densityFromRequest(request); got != tt.want {
				t.Errorf("densityFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

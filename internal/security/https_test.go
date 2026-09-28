package security

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestIsHTTPS(t *testing.T) {
	tests := []struct {
		name    string
		tls     bool
		headers map[string]string
		want    bool
	}{
		{name: "plain HTTP", want: false},
		{name: "direct TLS", tls: true, want: true},
		{name: "proxy says https", headers: map[string]string{"X-Forwarded-Proto": "https"}, want: true},
		{name: "proxy header is case-insensitive", headers: map[string]string{"X-Forwarded-Proto": "HTTPS"}, want: true},
		{name: "first hop of a proxy chain wins", headers: map[string]string{"X-Forwarded-Proto": "https, http"}, want: true},
		{name: "proxy says http", headers: map[string]string{"X-Forwarded-Proto": "http"}, want: false},
		{name: "standard Forwarded header", headers: map[string]string{"Forwarded": `for=192.0.2.60;proto=https;by=203.0.113.43`}, want: true},
		{name: "Forwarded with http", headers: map[string]string{"Forwarded": "for=192.0.2.60;proto=http"}, want: false},
		{name: "Forwarded quoted and uppercase", headers: map[string]string{"Forwarded": `Proto="HTTPS", proto=http`}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://example.test/", nil)
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := IsHTTPS(r); got != tt.want {
				t.Fatalf("IsHTTPS = %v, want %v", got, tt.want)
			}
		})
	}
}

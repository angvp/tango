package security

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "127.8.9.10": true, "::1": true,
		"localhost.example.com": false, "example.com": false, "10.0.0.1": false, "": false, "[::1]": false,
	} {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

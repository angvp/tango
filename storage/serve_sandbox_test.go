package storage_test

import (
	"net/http"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storagetest"
)

func TestServeSandboxPolicyCoversEveryObjectResponse(t *testing.T) {
	const wantCSP = "sandbox; default-src 'none'"
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	for _, tt := range []struct {
		name    string
		method  string
		headers map[string]string
		status  int
	}{
		{"HEAD", http.MethodHead, nil, http.StatusOK},
		{"a range", http.MethodGet, map[string]string{"Range": "bytes=0-3"}, http.StatusPartialContent},
		{"an unsatisfiable range", http.MethodGet, map[string]string{"Range": "bytes=9999-"}, http.StatusRequestedRangeNotSatisfiable},
		{"a conditional match", http.MethodGet, map[string]string{"If-None-Match": storage.ETagFor(obj.SHA256)}, http.StatusNotModified},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, store, obj.Key, storage.ServeOptions{}, tt.method, tt.headers)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("Content-Security-Policy"); got != wantCSP {
				t.Fatalf("Content-Security-Policy = %q, want %q", got, wantCSP)
			}
		})
	}
}

func TestServeSandboxPolicyIsAbsentFromErrorResponses(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	for _, tt := range []struct {
		name   string
		method string
		key    string
		status int
	}{
		{"a missing key", http.MethodGet, storage.NewKey(), http.StatusNotFound},
		{"a method not allowed", http.MethodPost, obj.Key, http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, store, tt.key, storage.ServeOptions{}, tt.method, nil)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d", rec.Code, tt.status)
			}
			if got, ok := rec.Header()["Content-Security-Policy"]; ok {
				t.Fatalf("Content-Security-Policy = %q on an error response, want none", got)
			}
		})
	}
}

func TestAnObjectDeletedBetweenStatAndOpenCarriesNoSandboxPolicy(t *testing.T) {
	memory := storagetest.NewMemory()
	obj := putObject(t, memory, serveBody)
	rec := serve(t, vanishingStore{memory}, obj.Key, storage.ServeOptions{}, http.MethodGet, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got, ok := rec.Header()["Content-Security-Policy"]; ok {
		t.Fatalf("Content-Security-Policy = %q on a 404, want none", got)
	}
}

func TestServeSendsTheSandboxPolicyByDefault(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	const wantCSP = "sandbox; default-src 'none'"
	if got := rec.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
}

func TestServeSkipSandboxPolicyOmitsOnlyTheHeader(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	defaulted := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, nil)
	skipped := serve(t, store, obj.Key, storage.ServeOptions{SkipSandboxPolicy: true}, http.MethodGet, nil)

	if got, ok := skipped.Header()["Content-Security-Policy"]; ok {
		t.Fatalf("Content-Security-Policy = %q with SkipSandboxPolicy, want none", got)
	}
	if skipped.Code != defaulted.Code || skipped.Body.String() != defaulted.Body.String() {
		t.Fatalf("status or body changed: %d/%d bytes vs %d/%d", skipped.Code, skipped.Body.Len(), defaulted.Code, defaulted.Body.Len())
	}
	want := defaulted.Header().Clone()
	want.Del("Content-Security-Policy")
	got := skipped.Header().Clone()
	for name := range want {
		if got.Get(name) != want.Get(name) {
			t.Errorf("%s = %q with SkipSandboxPolicy, want %q", name, got.Get(name), want.Get(name))
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s = %q added by SkipSandboxPolicy", name, got.Get(name))
		}
	}
}

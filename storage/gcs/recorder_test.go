package gcs_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recorder is a loopback HTTP server that counts what reaches it. A test
// points an environment variable or an endpoint at it to prove that a request
// did, or did not, go there.
type recorder struct {
	srv   *httptest.Server
	mu    sync.Mutex
	paths []string
	auth  []string
}

func newRecorder(t *testing.T, status int, body string) *recorder {
	t.Helper()
	r := &recorder{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.paths = append(r.paths, req.Method+" "+req.URL.Path)
		r.auth = append(r.auth, req.Header.Get("Authorization"))
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Metadata-Flavor", "Google")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// hostPort is the recorder's address without its scheme.
func (r *recorder) hostPort() string { return strings.TrimPrefix(r.srv.URL, "http://") }

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

// authorizations returns the Authorization header of each request received.
func (r *recorder) authorizations() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auth...)
}

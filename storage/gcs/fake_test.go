package gcs_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
)

const testBucket = "uploads"

// newFake starts fake-gcs-server on plain HTTP, the transport the adapter's
// HTTP/JSON client uses in these tests, with the test bucket ready.
func newFake(t *testing.T) *fakestorage.Server {
	t.Helper()
	s, err := fakestorage.NewServerWithOptions(fakestorage.Options{Scheme: "http"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	s.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: testBucket})
	return s
}

// wire sits between the adapter's client and the fake. It counts the requests
// that reach the fake, and can lose the response to the first uploads (the
// create has committed, the caller sees a 503) or answer every request with a
// fixed status without forwarding it.
type wire struct {
	base http.RoundTripper

	// loseUploads is how many upload responses to replace with a 503.
	loseUploads int
	// denyStatus, when set, answers every request with that status.
	denyStatus int
	// beforeMetadataRead, when set, runs before each metadata read is forwarded.
	beforeMetadataRead func()

	mu        sync.Mutex
	uploads   int // create-only uploads that reached the transport
	chunks    int // resumable upload chunks (requests that carry an upload_id)
	attrReads int // object metadata reads (GET without alt=media)
	lost      int
	statuses  []int // status the fake produced for each forwarded upload
}

type wireCounts struct {
	uploads, chunks, attrReads int
	statuses                   []int
}

func (w *wire) RoundTrip(req *http.Request) (*http.Response, error) {
	upload := req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/upload/")
	w.mu.Lock()
	if upload {
		w.uploads++
	}
	if upload && req.URL.Query().Get("upload_id") != "" {
		w.chunks++
	}
	metadataRead := req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/o/") && req.URL.Query().Get("alt") != "media"
	if metadataRead {
		w.attrReads++
	}
	hook := w.beforeMetadataRead
	w.mu.Unlock()
	if metadataRead && hook != nil {
		hook()
	}

	if w.denyStatus != 0 {
		return synthetic(req, w.denyStatus, `{"error":{"code":403,"message":"denied by the test wire"}}`), nil
	}
	resp, err := w.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	if upload {
		w.statuses = append(w.statuses, resp.StatusCode)
	}
	lose := upload && w.lost < w.loseUploads
	if lose {
		w.lost++
	}
	w.mu.Unlock()
	if !lose {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return synthetic(req, http.StatusServiceUnavailable, `{"error":{"code":503,"message":"response lost by the test wire"}}`), nil
}

func synthetic(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewBufferString(body)),
		Request:       req,
		ContentLength: int64(len(body)),
	}
}

func (w *wire) counts() wireCounts {
	w.mu.Lock()
	defer w.mu.Unlock()
	return wireCounts{uploads: w.uploads, chunks: w.chunks, attrReads: w.attrReads, statuses: append([]int(nil), w.statuses...)}
}

package storage_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"
	"github.com/angvp/tango/storagetest"
)

var serveBody = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00"), []byte("0123456789")...)

func putObject(t *testing.T, store storage.Store, data []byte) storage.Object {
	t.Helper()
	obj, err := store.Put(context.Background(), bytes.NewReader(data), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func serve(t *testing.T, store storage.Store, key string, opts storage.ServeOptions, method string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/files/x", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	if err := storage.Serve(rec, req, store, key, opts); err != nil {
		t.Fatalf("Serve returned %v", err)
	}
	return rec
}

func TestServeSendsTheObjectWithSafeHeaders(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), serveBody) {
		t.Fatalf("status %d, %d body bytes", rec.Code, rec.Body.Len())
	}
	h := rec.Header()
	for name, want := range map[string]string{
		"Content-Type":           "image/png",
		"Content-Length":         strconv.Itoa(len(serveBody)),
		"X-Content-Type-Options": "nosniff",
		"Accept-Ranges":          "bytes",
		"ETag":                   storage.ETagFor(obj.SHA256),
		"Cache-Control":          "private, no-store",
		"Content-Disposition":    "attachment",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if _, err := http.ParseTime(h.Get("Last-Modified")); err != nil {
		t.Errorf("Last-Modified = %q: %v", h.Get("Last-Modified"), err)
	}
}

func TestServeHonoursARangeAndRefusesAnUnsatisfiableOne(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	size := len(serveBody)
	total := strconv.Itoa(size)
	last := strconv.Itoa(size - 1)
	for _, tt := range []struct {
		name, header string
		status       int
		body         []byte
		contentRange string
	}{
		{"a prefix", "bytes=0-3", 206, serveBody[0:4], "bytes 0-3/" + total},
		{"an open end", "bytes=30-", 206, serveBody[30:], "bytes 30-" + last + "/" + total},
		{"a suffix", "bytes=-5", 206, serveBody[size-5:], "bytes " + strconv.Itoa(size-5) + "-" + last + "/" + total},
		{"an end past the object", "bytes=30-9999", 206, serveBody[30:], "bytes 30-" + last + "/" + total},
		{"a start past the object", "bytes=9999-", 416, nil, "bytes */" + total},
		{"a zero suffix", "bytes=-0", 416, nil, "bytes */" + total},
		{"several ranges get the whole object", "bytes=0-1,3-4", 200, serveBody, ""},
		{"an unknown unit is ignored", "items=0-1", 200, serveBody, ""},
		{"a malformed range is ignored", "bytes=a-b", 200, serveBody, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, map[string]string{"Range": tt.header})
			if rec.Code != tt.status || rec.Header().Get("Content-Range") != tt.contentRange {
				t.Fatalf("status %d, Content-Range %q; want %d, %q", rec.Code, rec.Header().Get("Content-Range"), tt.status, tt.contentRange)
			}
			if tt.body != nil && !bytes.Equal(rec.Body.Bytes(), tt.body) {
				t.Fatalf("body %q, want %q", rec.Body.Bytes(), tt.body)
			}
			if tt.status == 206 && rec.Header().Get("Content-Length") != strconv.Itoa(len(tt.body)) {
				t.Fatalf("Content-Length %q", rec.Header().Get("Content-Length"))
			}
		})
	}
}

func TestServeAnswersConditionalRequests(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	etag := storage.ETagFor(obj.SHA256)
	info, _ := store.Stat(context.Background(), obj.Key)
	modified := info.ModTime.UTC().Format(http.TimeFormat)
	before := info.ModTime.Add(-time.Hour).UTC().Format(http.TimeFormat)
	for _, tt := range []struct {
		name    string
		headers map[string]string
		status  int
	}{
		{"a matching If-None-Match", map[string]string{"If-None-Match": etag}, 304},
		{"a weak match", map[string]string{"If-None-Match": "W/" + etag}, 304},
		{"one of a list", map[string]string{"If-None-Match": `"other", ` + etag}, 304},
		{"a star", map[string]string{"If-None-Match": "*"}, 304},
		{"another ETag", map[string]string{"If-None-Match": `"other"`}, 200},
		{"an unmodified object", map[string]string{"If-Modified-Since": modified}, 304},
		{"a modified object", map[string]string{"If-Modified-Since": before}, 200},
		{"If-None-Match wins over If-Modified-Since", map[string]string{"If-None-Match": `"other"`, "If-Modified-Since": modified}, 200},
		{"If-Range with the current ETag keeps the range", map[string]string{"Range": "bytes=0-3", "If-Range": etag}, 206},
		{"If-Range with a stale ETag sends everything", map[string]string{"Range": "bytes=0-3", "If-Range": `"stale"`}, 200},
		{"If-Range with the current date keeps the range", map[string]string{"Range": "bytes=0-3", "If-Range": modified}, 206},
		{"If-Range with another date sends everything", map[string]string{"Range": "bytes=0-3", "If-Range": before}, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, tt.headers)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d", rec.Code, tt.status)
			}
			if rec.Code == 304 && (rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag) {
				t.Fatalf("304 with %d body bytes, ETag %q", rec.Body.Len(), rec.Header().Get("ETag"))
			}
		})
	}
}

func TestServeHeadSendsHeadersAndNoBody(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodHead, nil)
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != strconv.Itoa(len(serveBody)) || rec.Header().Get("ETag") == "" {
		t.Fatalf("status %d, body %d, headers %v", rec.Code, rec.Body.Len(), rec.Header())
	}
}

func TestServeOnlyAnswersGetAndHead(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodPost, nil)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("status %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestInlineIsOnlyForListedTypesAndNeverForActiveContent(t *testing.T) {
	store := storagetest.NewMemory()
	png := putObject(t, store, serveBody)
	html := putObject(t, store, []byte("<html><script>alert(1)</script></html>"))
	svg := putObject(t, store, []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	all := storage.ServeOptions{InlineTypes: []string{"image/*", "text/*", "text/html", "image/svg+xml"}}
	for _, tt := range []struct {
		name string
		key  string
		opts storage.ServeOptions
		want string
	}{
		{"a listed type", png.Key, storage.ServeOptions{InlineTypes: []string{"image/png"}}, "inline"},
		{"a wildcard-listed type", png.Key, storage.ServeOptions{InlineTypes: []string{"image/*"}}, "inline"},
		{"an unlisted type", png.Key, storage.ServeOptions{InlineTypes: []string{"application/pdf"}}, "attachment"},
		{"no list", png.Key, storage.ServeOptions{}, "attachment"},
		{"html even when listed", html.Key, all, "attachment"},
		{"svg even when listed", svg.Key, all, "attachment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, store, tt.key, tt.opts, http.MethodGet, nil)
			if got := rec.Header().Get("Content-Disposition"); got != tt.want {
				t.Fatalf("Content-Disposition = %q, want %q", got, tt.want)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("nosniff missing")
			}
		})
	}
}

func TestTheServedTypeIsTheStoredSniffedOne(t *testing.T) {
	store := storagetest.NewMemory()
	html := putObject(t, store, []byte("<html>x</html>"))
	rec := serve(t, store, html.Key, storage.ServeOptions{}, http.MethodGet, map[string]string{"Accept": "image/png"})
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestTheDispositionFilenameIsSanitizedAndEncoded(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	for _, tt := range []struct {
		name, filename, want string
	}{
		{"plain", "report.png", `attachment; filename="report.png"`},
		{"a path is reduced to its base", "../../etc/passwd", `attachment; filename="passwd"`},
		{"a windows path", `C:\Users\ada\cv.png`, `attachment; filename="cv.png"`},
		{"quotes and header injection", "a\"b\r\nSet-Cookie: x=1.png", `attachment; filename="a_bSet-Cookie_ x=1.png"`},
		{"non-ASCII gets an RFC 5987 form", "résumé.png", `attachment; filename="r_sum_.png"; filename*=UTF-8''r%C3%A9sum%C3%A9.png`},
		{"nothing usable is omitted", "\r\n", "attachment"},
		{"empty is omitted", "", "attachment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, store, obj.Key, storage.ServeOptions{Filename: tt.filename}, http.MethodGet, nil)
			got := rec.Header().Get("Content-Disposition")
			if strings.ContainsAny(got, "\r\n") {
				t.Fatalf("Content-Disposition has a line break: %q", got)
			}
			if got != tt.want {
				t.Fatalf("Content-Disposition = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCacheControlCanBeOverridden(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	rec := serve(t, store, obj.Key, storage.ServeOptions{CacheControl: "private, max-age=60"}, http.MethodGet, nil)
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=60" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestMissingAndInvalidKeysAreTheSame404(t *testing.T) {
	store := storagetest.NewMemory()
	missing := serve(t, store, storage.NewKey(), storage.ServeOptions{}, http.MethodGet, nil)
	invalid := serve(t, store, "../../etc/passwd", storage.ServeOptions{}, http.MethodGet, nil)
	if missing.Code != 404 || invalid.Code != 404 {
		t.Fatalf("statuses %d and %d, want 404", missing.Code, invalid.Code)
	}
	if missing.Body.String() != invalid.Body.String() || missing.Header().Get("Content-Type") != invalid.Header().Get("Content-Type") {
		t.Fatalf("the two 404s differ: %q vs %q", missing.Body.String(), invalid.Body.String())
	}
	if missing.Header().Get("Cache-Control") != "no-store" || missing.Header().Get("ETag") != "" {
		t.Fatalf("404 headers: %v", missing.Header())
	}
	deleted := putObject(t, store, serveBody)
	_ = store.Delete(context.Background(), deleted.Key)
	if gone := serve(t, store, deleted.Key, storage.ServeOptions{}, http.MethodGet, nil); gone.Body.String() != missing.Body.String() {
		t.Fatalf("a deleted object is distinguishable: %q", gone.Body.String())
	}
}

func TestServeReturnsUnexpectedStoreErrorsInsteadOfAnswering(t *testing.T) {
	rec := httptest.NewRecorder()
	err := storage.Serve(rec, httptest.NewRequest(http.MethodGet, "/", nil), failingStore{}, storage.NewKey(), storage.ServeOptions{})
	if err == nil || rec.Body.Len() != 0 {
		t.Fatalf("err = %v, body %q; want the error and no response", err, rec.Body.String())
	}
}

type failingStore struct{ storage.Store }

func (failingStore) Stat(context.Context, string) (storage.Info, error) {
	return storage.Info{}, os.ErrPermission
}

func TestAPartialLocalObjectIs404ThroughServe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	store, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	obj := putObject(t, store, serveBody)
	dir := filepath.Join(root, "objects", obj.Key[:2], obj.Key)
	if rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, nil); rec.Code != 200 {
		t.Fatalf("complete object: status %d", rec.Code)
	}
	for _, name := range []string{"meta", "data"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, nil); rec.Code != 404 {
			t.Fatalf("object missing %s: status %d, want 404", name, rec.Code)
		}
	}
}

// vanishingStore reports an object in Stat and loses it before Open, as a
// concurrent Delete would.
type vanishingStore struct{ storage.Store }

func (v vanishingStore) Open(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, storage.ErrNotFound
}

func TestAnObjectDeletedBetweenStatAndOpenIs404WithNoObjectHeaders(t *testing.T) {
	memory := storagetest.NewMemory()
	obj := putObject(t, memory, []byte("secret bytes"))
	rec := serve(t, vanishingStore{memory}, obj.Key, storage.ServeOptions{Filename: "secret.txt"}, "GET", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	for _, name := range []string{"ETag", "Last-Modified", "Content-Disposition", "Accept-Ranges", "Content-Length"} {
		if got := rec.Header().Get(name); got != "" {
			t.Errorf("%s = %q on a 404, want none", name, got)
		}
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q, want the JSON error's", rec.Header().Get("Content-Type"))
	}
}

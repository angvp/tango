package s3_test

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is a small in-process S3 server for the operations the adapter
// uses: path-style PutObject (with If-None-Match), multipart upload,
// GetObject with ranges, HeadObject and DeleteObject. It checks nothing about
// authentication. It exists so the conformance suite runs in CI with no
// container, no external service and no credentials.
type fakeS3 struct {
	bucket string

	mu       sync.Mutex
	objects  map[string]*fakeObject
	uploads  map[string]*fakeUpload
	requests int
	// Observations tests assert on.
	multipartStarted int
	conditionalPuts  int
	// Failure injection.
	failPart    int         // the UploadPart number that answers 403
	maxObject   int         // a PutObject larger than this answers EntityTooLarge
	denyHead    bool        // HeadObject answers 403 AccessDenied
	lastPutMeta http.Header // the stored headers of the last PutObject
	now         func() time.Time
}

type fakeObject struct {
	data     []byte
	headers  http.Header // Content-Type and X-Amz-Meta-*
	modified time.Time
}

type fakeUpload struct {
	name    string
	headers http.Header
	parts   map[int][]byte
}

func newFakeS3(t *testing.T, bucket string) (*fakeS3, *httptest.Server) {
	t.Helper()
	f := &fakeS3{bucket: bucket, objects: map[string]*fakeObject{}, uploads: map[string]*fakeUpload{}, now: time.Now}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, server
}

// seed stores an object the way a foreign writer would: with no metadata.
func (f *fakeS3) seed(name string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[name] = &fakeObject{data: data, headers: http.Header{"Content-Type": {"application/octet-stream"}}, modified: f.now()}
}

func (f *fakeS3) object(name string) (*fakeObject, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[name]
	return o, ok
}

func (f *fakeS3) count() (objects, uploads, requests int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects), len(f.uploads), f.requests
}

func (f *fakeS3) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++

	bucket, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket {
		f.fail(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
		f.fail(w, http.StatusNotImplemented, "NotImplemented") // the adapter must not send trailer checksums
		return
	}
	query := r.URL.Query()
	uploadID := query.Get("uploadId")

	switch {
	case r.Method == http.MethodPost && query.Has("uploads"):
		f.createUpload(w, r, name)
	case r.Method == http.MethodPut && uploadID != "":
		f.uploadPart(w, r, uploadID, query.Get("partNumber"))
	case r.Method == http.MethodPost && uploadID != "":
		f.completeUpload(w, r, uploadID)
	case r.Method == http.MethodDelete && uploadID != "":
		delete(f.uploads, uploadID)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		f.putObject(w, r, name)
	case r.Method == http.MethodDelete:
		delete(f.objects, name)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		f.getObject(w, r, name)
	default:
		f.fail(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// objectHeaders keeps the headers S3 stores with an object.
func objectHeaders(r *http.Request) http.Header {
	stored := http.Header{}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		stored.Set("Content-Type", ct)
	}
	for k, v := range r.Header {
		if strings.HasPrefix(k, "X-Amz-Meta-") {
			stored[k] = v
		}
	}
	return stored
}

func (f *fakeS3) putObject(w http.ResponseWriter, r *http.Request, name string) {
	if r.Header.Get("If-None-Match") == "*" {
		f.conditionalPuts++
		if _, exists := f.objects[name]; exists {
			f.fail(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		f.fail(w, http.StatusBadRequest, "IncompleteBody")
		return
	}
	if f.maxObject > 0 && len(data) > f.maxObject {
		f.fail(w, http.StatusBadRequest, "EntityTooLarge")
		return
	}
	f.lastPutMeta = objectHeaders(r)
	f.objects[name] = &fakeObject{data: data, headers: objectHeaders(r), modified: f.now()}
	w.Header().Set("ETag", etag(data))
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) createUpload(w http.ResponseWriter, r *http.Request, name string) {
	f.multipartStarted++
	id := fmt.Sprintf("upload-%d", f.multipartStarted)
	f.uploads[id] = &fakeUpload{name: name, headers: objectHeaders(r), parts: map[int][]byte{}}
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, f.bucket, name, id)
}

func (f *fakeS3) uploadPart(w http.ResponseWriter, r *http.Request, id, number string) {
	upload, ok := f.uploads[id]
	n, _ := strconv.Atoi(number)
	if !ok || n < 1 {
		f.fail(w, http.StatusNotFound, "NoSuchUpload")
		return
	}
	if f.failPart == n {
		f.fail(w, http.StatusForbidden, "AccessDenied")
		return
	}
	data, _ := io.ReadAll(r.Body)
	upload.parts[n] = data
	w.Header().Set("ETag", etag(data))
}

func (f *fakeS3) completeUpload(w http.ResponseWriter, r *http.Request, id string) {
	upload, ok := f.uploads[id]
	if !ok {
		f.fail(w, http.StatusNotFound, "NoSuchUpload")
		return
	}
	var request struct {
		Parts []struct {
			PartNumber int `xml:"PartNumber"`
		} `xml:"Part"`
	}
	if err := xml.NewDecoder(r.Body).Decode(&request); err != nil {
		f.fail(w, http.StatusBadRequest, "MalformedXML")
		return
	}
	if r.Header.Get("If-None-Match") == "*" {
		f.conditionalPuts++
		if _, exists := f.objects[upload.name]; exists {
			f.fail(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
	}
	numbers := make([]int, 0, len(request.Parts))
	for _, p := range request.Parts {
		numbers = append(numbers, p.PartNumber)
	}
	sort.Ints(numbers)
	var data bytes.Buffer
	for _, n := range numbers {
		part, ok := upload.parts[n]
		if !ok {
			f.fail(w, http.StatusBadRequest, "InvalidPart")
			return
		}
		data.Write(part)
	}
	f.objects[upload.name] = &fakeObject{data: data.Bytes(), headers: upload.headers, modified: f.now()}
	delete(f.uploads, id)
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><ETag>%s</ETag></CompleteMultipartUploadResult>`, f.bucket, upload.name, etag(data.Bytes()))
}

func (f *fakeS3) getObject(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method == http.MethodHead && f.denyHead {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	o, ok := f.objects[name]
	if !ok {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.fail(w, http.StatusNotFound, "NoSuchKey")
		return
	}
	for k, v := range o.headers {
		w.Header()[k] = v
	}
	w.Header().Set("ETag", etag(o.data))
	http.ServeContent(w, r, "", o.modified, bytes.NewReader(o.data))
}

func etag(data []byte) string {
	sum := md5.Sum(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

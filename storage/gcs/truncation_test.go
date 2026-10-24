package gcs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
)

var errSpoolRead = errors.New("test: spool read failed")

// failAfter reads up to n bytes from r, then fails.
type failAfter struct {
	r io.Reader
	n int
}

func (f *failAfter) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errSpoolRead
	}
	if len(p) > f.n {
		p = p[:f.n]
	}
	k, err := f.r.Read(p)
	f.n -= k
	return k, err
}

// A read failure partway through the spool copy must leave no object. The
// upload is fully visible or absent: a copy that stops early must not be
// finalized with the SHA-256 of the full content.
func TestAFailedSpoolCopyCommitsNoObject(t *testing.T) {
	restore := gcs.SetSpoolSource(func(f *os.File, size int64) io.Reader {
		return &failAfter{r: io.NewSectionReader(f, 0, size), n: 512 << 10}
	})
	defer restore()

	server := newFake(t)
	s, err := gcs.NewWithHTTPClient(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), ChunkSize: 256 << 10, TempDir: t.TempDir()}, &http.Client{Transport: http.DefaultTransport})
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("a"), 2<<20)
	_, err = s.Put(context.Background(), bytes.NewReader(content), storage.PutOptions{MaxSize: 4 << 20})
	if err == nil {
		t.Fatal("Put succeeded although the spool copy failed partway")
	}
	objs, _, lerr := server.ListObjects(testBucket, "", "", false)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(objs) != 0 {
		t.Fatalf("a failed upload left %d object(s) behind, first %q", len(objs), objs[0].Name)
	}
}

// A source failure during the spool copy is returned with the package and the
// stage named, and keeps its own error reachable.
func TestAFailedSourceReadNamesTheSpoolStage(t *testing.T) {
	server := newFake(t)
	s, err := gcs.NewWithHTTPClient(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), TempDir: t.TempDir()}, &http.Client{Transport: http.DefaultTransport})
	if err != nil {
		t.Fatal(err)
	}
	src := &failAfter{r: bytes.NewReader(make([]byte, 1024)), n: 512}
	_, err = s.Put(context.Background(), src, storage.PutOptions{MaxSize: 4 << 20})
	if !errors.Is(err, errSpoolRead) || !strings.HasPrefix(err.Error(), "storage/gcs: spool: ") {
		t.Fatalf("Put = %v; want storage/gcs: spool: wrapping the source error", err)
	}
}

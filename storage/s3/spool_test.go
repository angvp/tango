package s3_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

var errSourceRead = errors.New("test: source read failed")

type brokenSource struct{ r io.Reader }

func (b *brokenSource) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, errSourceRead
	}
	return n, err
}

// A source failure during the spool copy is returned with the package and the
// stage named, and keeps its own error reachable.
func TestAFailedSourceReadNamesTheSpoolStage(t *testing.T) {
	_, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	_, err := s.Put(context.Background(), &brokenSource{r: bytes.NewReader(bytes.Repeat([]byte("a"), 1024))}, storage.PutOptions{MaxSize: 4096})
	if !errors.Is(err, errSourceRead) || !strings.HasPrefix(err.Error(), "storage/s3: spool: ") {
		t.Fatalf("Put = %v; want storage/s3: spool: wrapping the source error", err)
	}
}

// An input over MaxSize is still a *storage.TooLargeError after the spool
// wraps it.
func TestAnOversizeInputStaysATooLargeError(t *testing.T) {
	_, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	_, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 2000)), storage.PutOptions{MaxSize: 1000})
	var tooLarge *storage.TooLargeError
	if !errors.Is(err, storage.ErrTooLarge) || !errors.As(err, &tooLarge) || tooLarge.Max != 1000 {
		t.Fatalf("Put = %v; want ErrTooLarge with Max 1000", err)
	}
}

var errSpoolInjected = errors.New("test: injected spool failure")

// failingClose fails the spool's close and still performs it, so no handle
// leaks.
func failingClose() storagekit.FileOps {
	return storagekit.FileOps{Close: func(f *os.File) error { _ = f.Close(); return errSpoolInjected }}
}

// A spool failure decided before the upload is returned with no request sent.
func TestASpoolFailureBeforeTheUploadSendsNoRequest(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	s.SetSpoolOps(failingClose())

	_, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
	if !errors.Is(err, errSpoolInjected) {
		t.Fatalf("Put = %v, want the spool close failure", err)
	}
	if objects, uploads, requests := fake.count(); requests != 0 || uploads != 0 || objects != 0 {
		t.Fatalf("after a failed spool: %d requests, %d uploads, %d objects; want none", requests, uploads, objects)
	}
}

// A cleanup failure after a committed upload must not turn it into an error.
func TestAFailedCleanupAfterACommitReturnsTheObject(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "app", nil)
	var releases int
	s.SetReleaseSpool(func(sp *storagekit.SpoolFile) error {
		releases++
		_ = sp.Release()
		return errSpoolInjected
	})

	obj, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatalf("Put after a commit with failed cleanup = %v, want the object", err)
	}
	if objects, _, _ := fake.count(); objects != 1 {
		t.Fatalf("bucket holds %d objects, want exactly 1", objects)
	}
	if _, ok := fake.object("app/" + obj.Key); !ok {
		t.Fatalf("object app/%s missing", obj.Key)
	}
	if releases != 1 {
		t.Fatalf("cleanup attempted %d times, want 1", releases)
	}
}

// Cleanup is attempted on success, on a pre-upload failure and on an upload
// failure.
func TestS3CleanupIsAttemptedOnSuccessAndOnEveryFailure(t *testing.T) {
	cases := map[string]struct {
		ops    storagekit.FileOps
		fail   bool
		wantOK bool
	}{
		"success":            {wantOK: true},
		"pre-upload failure": {ops: failingClose()},
		"upload failure":     {fail: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fake, server := newFakeS3(t, testBucket)
			if c.fail {
				server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fake.fail(w, http.StatusForbidden, "AccessDenied")
				})
			}
			s := newStore(t, server.URL, "", nil)
			s.SetSpoolOps(c.ops)
			var releases int
			s.SetReleaseSpool(func(sp *storagekit.SpoolFile) error {
				releases++
				return sp.Release()
			})

			_, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
			if (err == nil) != c.wantOK {
				t.Fatalf("Put = %v, wantOK %v", err, c.wantOK)
			}
			if releases != 1 {
				t.Fatalf("cleanup attempted %d times, want 1", releases)
			}
		})
	}
}

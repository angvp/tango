package gcs_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

var errSpoolInjected = errors.New("test: injected spool failure")

// failingClose fails the spool's close and still performs it, so no handle
// leaks.
func failingClose() storagekit.FileOps {
	return storagekit.FileOps{Close: func(f *os.File) error { _ = f.Close(); return errSpoolInjected }}
}

// A spool failure that is decided before the upload must be returned with no
// upload request sent: an upload never starts on bytes that did not reach the
// file.
func TestASpoolFailureBeforeTheUploadSendsNoUploadRequest(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "")
	s.SetSpoolOps(failingClose())

	_, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
	if !errors.Is(err, errSpoolInjected) {
		t.Fatalf("Put = %v, want the spool close failure", err)
	}
	if c := w.counts(); c.uploads != 0 {
		t.Fatalf("%d upload requests reached the fake after a failed spool, want 0", c.uploads)
	}
	objs, _, err := server.ListObjects(testBucket, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 0 {
		t.Fatalf("bucket holds %d objects after a failed spool, want 0", len(objs))
	}
}

// A cleanup failure after a committed upload must not turn the upload into an
// error: the caller gets the object, and exactly one object exists.
func TestAFailedCleanupAfterACommitReturnsTheObject(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "app")
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
	objs, _, err := server.ListObjects(testBucket, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Name != "app/"+obj.Key {
		t.Fatalf("bucket holds %d objects, want exactly app/%s", len(objs), obj.Key)
	}
	if releases != 1 {
		t.Fatalf("cleanup attempted %d times, want 1", releases)
	}
}

// Cleanup is attempted on success, on a pre-upload failure and on an upload
// failure.
func TestCleanupIsAttemptedOnSuccessAndOnEveryFailure(t *testing.T) {
	cases := map[string]struct {
		ops    storagekit.FileOps
		deny   int
		wantOK bool
	}{
		"success":            {wantOK: true},
		"pre-upload failure": {ops: failingClose()},
		"upload failure":     {deny: http.StatusForbidden},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			server := newFake(t)
			w := &wire{base: http.DefaultTransport, denyStatus: c.deny}
			s := newWired(t, server, w, "")
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

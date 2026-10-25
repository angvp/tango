package gcs_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/googleapi"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
)

func sum(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// newWired returns a Store whose client talks to server through w.
func newWired(t *testing.T, server *fakestorage.Server, w *wire, prefix string) *gcs.Store {
	t.Helper()
	s, err := gcs.NewWithHTTPClient(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), Prefix: prefix, TempDir: t.TempDir()}, &http.Client{Transport: w})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRetriedCommittedCreateReconcilesToTheOriginalKey(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport, dropUploads: 1}
	s := newWired(t, server, w, "app")
	const content = "reconciled upload"
	const key = "abcdefghijklmnopqrstuvwxyz234567"
	var keysIssued int
	s.SetKeyFunc(func() string { keysIssued++; return key })

	obj, err := s.Put(context.Background(), strings.NewReader(content), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if obj.Key != key {
		t.Fatalf("Put returned key %q, want the original %q", obj.Key, key)
	}
	if keysIssued != 1 {
		t.Fatalf("keys issued = %d, want 1: the retry must not pick a new key", keysIssued)
	}
	c := w.counts()
	if len(c.statuses) != 2 || c.statuses[0] != http.StatusOK || c.statuses[1] != http.StatusPreconditionFailed {
		t.Fatalf("fake answered %v, want [200 412]: the first create committed, the retry met it", c.statuses)
	}
	if c.attrReads != 1 {
		t.Fatalf("attribute reads = %d, want exactly 1", c.attrReads)
	}
	objs, _, err := server.ListObjects(testBucket, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Name != "app/"+key {
		t.Fatalf("bucket holds %d objects, want one at app/%s", len(objs), key)
	}
	stored, err := server.GetObject(testBucket, "app/"+key)
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.Metadata["sha256"]; got != sum(content) || got != obj.SHA256 {
		t.Fatalf("stored sha256 = %q, want %q", got, sum(content))
	}
}

func TestACollidingKeyIsRetriedWithANewOneAndNeverOverwritten(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "")
	const taken = "abcdefghijklmnopqrstuvwxyz234567"
	const fresh = "zyxwvutsrqponmlkjihgfedcba234567"
	const other = "someone else's bytes"
	server.CreateObject(fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: taken, ContentType: "text/plain", Metadata: map[string]string{"sha256": sum(other)}},
		Content:     []byte(other),
	})
	keys := []string{taken, fresh}
	s.SetKeyFunc(func() string { k := keys[0]; keys = keys[1:]; return k })

	const content = "my own bytes"
	obj, err := s.Put(context.Background(), strings.NewReader(content), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if obj.Key != fresh {
		t.Fatalf("Put returned key %q, want the fresh %q", obj.Key, fresh)
	}
	if c := w.counts(); c.attrReads != 1 {
		t.Fatalf("attribute reads = %d, want 1 for the one conflict", c.attrReads)
	}
	kept, err := server.GetObject(testBucket, taken)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept.Content) != other || kept.Metadata["sha256"] != sum(other) {
		t.Fatalf("the colliding object changed: %q", kept.Content)
	}
}

func TestExhaustedCollisionsReturnErrExists(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "")
	const taken = "abcdefghijklmnopqrstuvwxyz234567"
	server.CreateObject(fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: taken, ContentType: "text/plain", Metadata: map[string]string{"sha256": sum("other")}},
		Content:     []byte("other"),
	})
	s.SetKeyFunc(func() string { return taken })

	_, err := s.Put(context.Background(), strings.NewReader("mine"), storage.PutOptions{MaxSize: 1 << 20})
	if !errors.Is(err, storage.ErrExists) {
		t.Fatalf("Put = %v, want an error wrapping storage.ErrExists", err)
	}
	if c := w.counts(); c.attrReads != 3 || c.uploads != 3 {
		t.Fatalf("uploads %d, attribute reads %d; want 3 and 3", c.uploads, c.attrReads)
	}
}

func TestAnOversizeStreamIsRefusedBeforeAnyUploadRequest(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "")

	_, err := s.Put(context.Background(), strings.NewReader(strings.Repeat("a", 2000)), storage.PutOptions{MaxSize: 1000})
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("Put = %v, want ErrTooLarge", err)
	}
	if c := w.counts(); c.uploads != 0 {
		t.Fatalf("%d upload requests reached the fake for an oversize stream, want 0", c.uploads)
	}
}

func TestOtherFailuresAreWrappedWithTheOperationAndKeepTheSDKError(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport, denyStatus: http.StatusForbidden}
	s := newWired(t, server, w, "")
	key := "abcdefghijklmnopqrstuvwxyz234567"

	_, err := s.Stat(context.Background(), key)
	var g *googleapi.Error
	if !errors.As(err, &g) || g.Code != http.StatusForbidden {
		t.Fatalf("Stat = %v, want the SDK's 403 reachable with errors.As", err)
	}
	if !strings.HasPrefix(err.Error(), "storage/gcs: stat: ") {
		t.Fatalf("Stat error %q lacks the operation prefix", err)
	}

	_, err = s.Put(context.Background(), bytes.NewReader([]byte("x")), storage.PutOptions{MaxSize: 10})
	if !errors.As(err, &g) || !strings.HasPrefix(err.Error(), "storage/gcs: put: ") {
		t.Fatalf("Put = %v, want the wrapped SDK 403 with the put prefix", err)
	}
}

func TestAVanishedObjectAfterA412RetriesTheSameKey(t *testing.T) {
	server := newFake(t)
	const key = "abcdefghijklmnopqrstuvwxyz234567"
	const fresh = "zyxwvutsrqponmlkjihgfedcba234567"
	const content = "my own bytes"
	const other = "someone else's bytes"
	server.CreateObject(fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: key, ContentType: "text/plain", Metadata: map[string]string{"sha256": sum(other)}},
		Content:     []byte(other),
	})
	w := &wire{base: http.DefaultTransport}
	// The object is removed after the 412 and before the attribute read, so
	// the read finds nothing: the key is free again.
	w.beforeMetadataRead = func() {
		req, err := http.NewRequest(http.MethodDelete, server.URL()+"/storage/v1/b/"+testBucket+"/o/"+key, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	s := newWired(t, server, w, "")
	keys := []string{key, fresh}
	var keysIssued int
	s.SetKeyFunc(func() string { keysIssued++; k := keys[0]; keys = keys[1:]; return k })

	obj, err := s.Put(context.Background(), strings.NewReader(content), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if obj.Key != key {
		t.Fatalf("Put returned key %q, want the same key %q: a vanished object frees its key for a retry", obj.Key, key)
	}
	if keysIssued != 1 {
		t.Fatalf("keys issued = %d, want 1: the vanished case retries the same key", keysIssued)
	}
	if c := w.counts(); len(c.statuses) != 2 || c.statuses[0] != http.StatusPreconditionFailed || c.statuses[1] != http.StatusOK {
		t.Fatalf("upload statuses %v, want [412 200]", c.statuses)
	}
	objs, _, err := server.ListObjects(testBucket, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Name != key {
		t.Fatalf("bucket holds %d objects, want exactly the one at %s", len(objs), key)
	}
	stored, err := server.GetObject(testBucket, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Content) != content || stored.Metadata["sha256"] != sum(content) {
		t.Fatalf("stored object %q, want the uploaded content", stored.Content)
	}
}

// TestAForeignObjectNeverPassesAsThisWritesRetry pre-seeds the key the Put will
// try first with an object whose SHA-256 metadata equals the Put's. Only a
// matching write token, size and SHA-256 identify this write's own object, so
// each variant must be refused: Put picks a new key and leaves the foreign
// object exactly as it was.
func TestAForeignObjectNeverPassesAsThisWritesRetry(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	const content = "my own bytes" // 12 bytes
	const taken = "abcdefghijklmnopqrstuvwxyz234567"
	const fresh = "zyxwvutsrqponmlkjihgfedcba234567"
	cases := map[string]struct {
		metadata map[string]string
		body     string
	}{
		"right token, wrong size": {
			metadata: map[string]string{"sha256": sum(content), gcs.TokenMetadata: token},
			body:     "a longer foreign body",
		},
		"right SHA-256 and size, no token": {
			metadata: map[string]string{"sha256": sum(content)},
			body:     "foreign body",
		},
		"right SHA-256 and size, wrong token": {
			metadata: map[string]string{"sha256": sum(content), gcs.TokenMetadata: "fedcba9876543210fedcba9876543210"},
			body:     "foreign body",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			server := newFake(t)
			w := &wire{base: http.DefaultTransport}
			s := newWired(t, server, w, "")
			s.SetWriteTokenFunc(func() (string, error) { return token, nil })
			server.CreateObject(fakestorage.Object{
				ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: taken, ContentType: "text/plain", Metadata: c.metadata},
				Content:     []byte(c.body),
			})
			keys := []string{taken, fresh}
			s.SetKeyFunc(func() string { k := keys[0]; keys = keys[1:]; return k })

			obj, err := s.Put(context.Background(), strings.NewReader(content), storage.PutOptions{MaxSize: 1 << 20})
			if err != nil {
				t.Fatalf("Put: %v", err)
			}
			if obj.Key != fresh {
				t.Fatalf("Put returned key %q, want the fresh %q: a foreign object is not this write's retry", obj.Key, fresh)
			}
			kept, err := server.GetObject(testBucket, taken)
			if err != nil {
				t.Fatal(err)
			}
			if string(kept.Content) != c.body {
				t.Fatalf("the foreign object's bytes changed to %q", kept.Content)
			}
			for k, v := range c.metadata {
				if kept.Metadata[k] != v {
					t.Fatalf("the foreign object's metadata %q changed to %q", k, kept.Metadata[k])
				}
			}
		})
	}
}

// Without the lost response the create is made once and never retried, so the
// reconciliation path is not taken.
func TestWithoutALostResponseTheCreateIsSentOnce(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "")
	obj, err := s.Put(context.Background(), strings.NewReader("single upload"), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	c := w.counts()
	if len(c.statuses) != 1 || c.statuses[0] != http.StatusOK {
		t.Fatalf("fake answered %v, want exactly [200]", c.statuses)
	}
	if c.attrReads != 0 {
		t.Fatalf("attribute reads = %d, want 0 on a first-try create", c.attrReads)
	}
	objs, _, err := server.ListObjects(testBucket, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Name != obj.Key {
		t.Fatalf("bucket holds %d objects, want exactly the one at %s", len(objs), obj.Key)
	}
}

// The write token is internal: Stat reports the object without it.
func TestStatDoesNotExposeTheWriteToken(t *testing.T) {
	server := newFake(t)
	w := &wire{base: http.DefaultTransport}
	s := newWired(t, server, w, "")
	const token = "0123456789abcdef0123456789abcdef"
	s.SetWriteTokenFunc(func() (string, error) { return token, nil })
	obj, err := s.Put(context.Background(), strings.NewReader("visible"), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := s.Stat(context.Background(), obj.Key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", info), token) {
		t.Fatalf("Stat exposes the write token: %+v", info)
	}
}

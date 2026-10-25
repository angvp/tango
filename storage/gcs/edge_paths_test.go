package gcs_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsouza/fake-gcs-server/fakestorage"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
)

const (
	keyA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	keyB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// fixedToken is the write token the tests inject, so a seeded object can
	// carry the same token as the write that meets it.
	fixedToken = "tok-fixed"
	// shaKey is the object metadata key holding the SHA-256 of the content.
	shaKey = "sha256"
)

var (
	errStageInjected = errors.New("test: injected staging failure")
	errTokenInjected = errors.New("test: injected token failure")
)

// answerFunc returns the response a test substitutes for req. Nil forwards req
// to the fake.
type answerFunc func(req *http.Request) *http.Response

// answerWire sits between the adapter and the fake. It counts every request
// that reaches the transport and every create-only upload, and lets a test
// answer selected requests itself.
type answerWire struct {
	base   http.RoundTripper
	answer answerFunc

	mu       sync.Mutex
	requests int
	uploads  int
}

func (w *answerWire) RoundTrip(req *http.Request) (*http.Response, error) {
	w.mu.Lock()
	w.requests++
	if isCreateUpload(req) {
		w.uploads++
	}
	w.mu.Unlock()
	if w.answer != nil {
		if resp := w.answer(req); resp != nil {
			return resp, nil
		}
	}
	return w.base.RoundTrip(req)
}

func (w *answerWire) counts() (requests, uploads int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.requests, w.uploads
}

func isCreateUpload(req *http.Request) bool {
	return req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/upload/")
}

func isMetadataRead(req *http.Request) bool {
	return req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/o/") && req.URL.Query().Get("alt") != "media"
}

func isMediaRead(req *http.Request) bool {
	return req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/o/") && req.URL.Query().Get("alt") == "media"
}

func isObjectDelete(req *http.Request) bool {
	return req.Method == http.MethodDelete && strings.Contains(req.URL.Path, "/o/")
}

// newAnswered returns a Store wired through an answerWire, with the fake
// server it forwards to. tempDir is the spool directory.
func newAnswered(t *testing.T, tempDir string, answer answerFunc) (*fakestorage.Server, *gcs.Store, *answerWire) {
	t.Helper()
	server := newFake(t)
	w := &answerWire{base: http.DefaultTransport, answer: answer}
	s, err := gcs.NewWithHTTPClient(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), TempDir: tempDir}, &http.Client{Transport: w})
	if err != nil {
		t.Fatal(err)
	}
	return server, s, w
}

// seedObject places an object at key in the test bucket with the given
// metadata and content, as an earlier write would have left it.
func seedObject(t *testing.T, server *fakestorage.Server, key string, metadata map[string]string, content string) {
	t.Helper()
	server.CreateObject(fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: key, Metadata: metadata},
		Content:     []byte(content),
	})
}

// objectNames lists the object names in the test bucket.
func objectNames(t *testing.T, server *fakestorage.Server) []string {
	t.Helper()
	objs, _, err := server.ListObjects(testBucket, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(objs))
	for _, o := range objs {
		names = append(names, o.Name)
	}
	return names
}

// A Put whose staging, token or key generation fails must return that failure
// before any upload request is sent.
func TestPutFailsBeforeAnyUploadWhenStagingFails(t *testing.T) {
	cases := []struct {
		name    string
		tempDir func(t *testing.T) string
		prepare func(s *gcs.Store)
		wantIs  error
	}{
		{
			name:    "spool directory does not exist",
			tempDir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") },
		},
		{
			name:    "spool file removed before it is reopened",
			tempDir: func(t *testing.T) string { return t.TempDir() },
			prepare: func(s *gcs.Store) {
				s.SetSpoolOps(storagekit.FileOps{Sync: func(f *os.File) error { return os.Remove(f.Name()) }})
			},
		},
		{
			name:    "write token generation fails",
			tempDir: func(t *testing.T) string { return t.TempDir() },
			prepare: func(s *gcs.Store) {
				s.SetWriteTokenFunc(func() (string, error) { return "", errTokenInjected })
			},
			wantIs: errTokenInjected,
		},
		{
			name:    "generated key fails validation",
			tempDir: func(t *testing.T) string { return t.TempDir() },
			prepare: func(s *gcs.Store) {
				s.SetKeyFunc(func() string { return "not-a-valid-key" })
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, s, w := newAnswered(t, tc.tempDir(t), nil)
			if tc.prepare != nil {
				tc.prepare(s)
			}

			_, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
			if err == nil {
				t.Fatal("Put succeeded, want an error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("Put = %v, want %v", err, tc.wantIs)
			}
			if _, uploads := w.counts(); uploads != 0 {
				t.Fatalf("%d upload requests were sent, want 0", uploads)
			}
			if names := objectNames(t, server); len(names) != 0 {
				t.Fatalf("bucket holds %v after a failed Put, want nothing", names)
			}
		})
	}
}

// A 412 whose reconciling read fails must surface that read's failure. It must
// not be reported as a key collision, and no second create is sent.
func TestPutReturnsTheReconcileFailureNotACollision(t *testing.T) {
	answer := func(req *http.Request) *http.Response {
		switch {
		case isCreateUpload(req):
			return synthetic(req, http.StatusPreconditionFailed, `{"error":{"code":412,"message":"conditionNotMet"}}`)
		case isMetadataRead(req):
			return synthetic(req, http.StatusForbidden, `{"error":{"code":403,"message":"denied by the test wire"}}`)
		}
		return nil
	}
	_, s, w := newAnswered(t, t.TempDir(), answer)

	_, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
	if err == nil {
		t.Fatal("Put succeeded, want the reconcile failure")
	}
	if errors.Is(err, storage.ErrExists) {
		t.Fatalf("a failed reconcile read was reported as a collision: %v", err)
	}
	if !strings.Contains(err.Error(), "storage/gcs: put") {
		t.Fatalf("error %q does not name the put operation", err)
	}
	if _, uploads := w.counts(); uploads != 1 {
		t.Fatalf("%d create requests, want exactly 1 with no retry", uploads)
	}
}

// A 412 is retried under a new key only when the object at the key is not this
// write's own. Each row differs from this write in exactly one of token, SHA-256
// or size, and the foreign object must be left untouched.
func TestPutRetriesUnderANewKeyWhenTheConflictingObjectDiffersInAnyField(t *testing.T) {
	const payload = "payload"
	cases := []struct {
		name     string
		metadata map[string]string
		content  string
	}{
		{
			name:     "token differs",
			metadata: map[string]string{shaKey: sum(payload), gcs.TokenMetadata: "tok-other"},
			content:  payload,
		},
		{
			name:     "sha256 differs",
			metadata: map[string]string{shaKey: sum("other"), gcs.TokenMetadata: fixedToken},
			content:  payload,
		},
		{
			name:     "size differs",
			metadata: map[string]string{shaKey: sum(payload), gcs.TokenMetadata: fixedToken},
			content:  payload + "X",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, s, w := newAnswered(t, t.TempDir(), nil)
			keys := []string{keyA, keyB}
			s.SetKeyFunc(func() string {
				k := keys[0]
				keys = keys[1:]
				return k
			})
			s.SetWriteTokenFunc(func() (string, error) { return fixedToken, nil })
			seedObject(t, server, keyA, tc.metadata, tc.content)

			obj, err := s.Put(context.Background(), strings.NewReader(payload), storage.PutOptions{MaxSize: 1 << 20})
			if err != nil {
				t.Fatalf("Put = %v, want success under the second key", err)
			}
			if obj.Key != keyB {
				t.Fatalf("Put key = %q, want %q", obj.Key, keyB)
			}
			if _, uploads := w.counts(); uploads != 2 {
				t.Fatalf("%d create requests, want 2 (conflict, then the new key)", uploads)
			}
			foreign, err := server.GetObject(testBucket, keyA)
			if err != nil {
				t.Fatal(err)
			}
			if string(foreign.Content) != tc.content {
				t.Fatalf("foreign object content = %q, want it untouched at %q", foreign.Content, tc.content)
			}
		})
	}
}

// Stat, Open, Delete and Put fail the same way when the credentials are
// unusable, and no operation echoes the credential document.
func TestCredentialFailuresOnEveryOperationNeverEchoTheSecret(t *testing.T) {
	isolateAmbientCredentials(t)
	ops := map[string]func(ctx context.Context, s *gcs.Store) error{
		"Put": func(ctx context.Context, s *gcs.Store) error {
			_, err := s.Put(ctx, strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
			return err
		},
		"Stat": func(ctx context.Context, s *gcs.Store) error {
			_, err := s.Stat(ctx, keyA)
			return err
		},
		"Open": func(ctx context.Context, s *gcs.Store) error {
			_, err := s.Open(ctx, keyA, 0, -1)
			return err
		},
		"Delete": func(ctx context.Context, s *gcs.Store) error {
			return s.Delete(ctx, keyA)
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			s, err := gcs.New(context.Background(), gcs.Config{
				Bucket:          testBucket,
				CredentialsJSON: []byte(`{"type":"service_account","private_key":"` + secretMarker + `"`),
				TempDir:         t.TempDir(),
			})
			if err != nil {
				if strings.Contains(err.Error(), secretMarker) {
					t.Fatalf("constructor error echoes a credential: %v", err)
				}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = op(ctx, s)
			if err == nil {
				t.Fatal("operation succeeded with unusable credentials")
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Fatalf("operation error echoes a credential: %v", err)
			}
		})
	}
}

// A read of a range the fake refuses maps to the storage sentinel, and any
// other refusal keeps its operation and cause without becoming a range error.
func TestOpenMapsRangeRefusalsToTheirSentinel(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantIs  error
		notIs   error
		wantMsg string
	}{
		{
			name:   "416 is an invalid range",
			status: http.StatusRequestedRangeNotSatisfiable,
			body:   `{"error":{"code":416,"message":"requested range not satisfiable"}}`,
			wantIs: storage.ErrInvalidRange,
		},
		{
			name:    "403 is a plain open failure",
			status:  http.StatusForbidden,
			body:    `{"error":{"code":403,"message":"denied by the test wire"}}`,
			notIs:   storage.ErrInvalidRange,
			wantMsg: "storage/gcs: open",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer := func(req *http.Request) *http.Response {
				if isMediaRead(req) {
					return synthetic(req, tc.status, tc.body)
				}
				return nil
			}
			server, s, _ := newAnswered(t, t.TempDir(), answer)
			seedObject(t, server, keyA, map[string]string{shaKey: sum("hello")}, "hello")

			r, err := s.Open(context.Background(), keyA, 0, -1)
			if err == nil {
				_ = r.Close()
				t.Fatal("Open succeeded, want the refusal")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("Open = %v, want %v", err, tc.wantIs)
			}
			if tc.notIs != nil && errors.Is(err, tc.notIs) {
				t.Fatalf("Open = %v, must not be %v", err, tc.notIs)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("Open = %q, want it to name %q", err, tc.wantMsg)
			}
		})
	}
}

// Delete wraps a refused request with its operation, and treats an object that
// is already gone as deleted.
func TestDeleteWrapsARefusalAndAcceptsAMissingObject(t *testing.T) {
	t.Run("refused delete names the operation", func(t *testing.T) {
		answer := func(req *http.Request) *http.Response {
			if isObjectDelete(req) {
				return synthetic(req, http.StatusForbidden, `{"error":{"code":403,"message":"denied by the test wire"}}`)
			}
			return nil
		}
		server, s, _ := newAnswered(t, t.TempDir(), answer)
		seedObject(t, server, keyA, map[string]string{shaKey: sum("hello")}, "hello")

		err := s.Delete(context.Background(), keyA)
		if err == nil {
			t.Fatal("Delete succeeded, want the refusal")
		}
		if !strings.Contains(err.Error(), "storage/gcs: delete") {
			t.Fatalf("Delete = %q, want it to name the delete operation", err)
		}
		if names := objectNames(t, server); len(names) != 1 {
			t.Fatalf("refused delete left %v, want the object still present", names)
		}
	})

	t.Run("missing object is not an error", func(t *testing.T) {
		_, s, _ := newAnswered(t, t.TempDir(), nil)
		if err := s.Delete(context.Background(), keyA); err != nil {
			t.Fatalf("Delete of a missing object = %v, want nil", err)
		}
	})
}

// A key that is not in the NewKey form is refused before any request is sent,
// on every operation that takes one.
func TestRejectedKeysNeverReachTheWire(t *testing.T) {
	keys := map[string]string{
		"empty":            "",
		"too short":        "aaaa",
		"uppercase":        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"path traversal":   "../../etc/passwd",
		"invalid symbol":   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa!",
		"separator inside": "aaaaaaaaaaaaaaaaaaaaaaaa/aaaaaa",
	}
	ops := map[string]func(ctx context.Context, s *gcs.Store, key string) error{
		"Stat": func(ctx context.Context, s *gcs.Store, key string) error {
			_, err := s.Stat(ctx, key)
			return err
		},
		"Open": func(ctx context.Context, s *gcs.Store, key string) error {
			_, err := s.Open(ctx, key, 0, -1)
			return err
		},
		"Delete": func(ctx context.Context, s *gcs.Store, key string) error {
			return s.Delete(ctx, key)
		},
	}
	for opName, op := range ops {
		for keyName, key := range keys {
			t.Run(opName+"/"+keyName, func(t *testing.T) {
				_, s, w := newAnswered(t, t.TempDir(), nil)
				err := op(context.Background(), s, key)
				if !errors.Is(err, storage.ErrInvalidKey) {
					t.Fatalf("%s(%q) = %v, want storage.ErrInvalidKey", opName, key, err)
				}
				if requests, _ := w.counts(); requests != 0 {
					t.Fatalf("%d requests reached the transport for a rejected key, want 0", requests)
				}
			})
		}
	}
}

// A context canceled before Put starts, or while its upload request is being
// issued, must not leave a committed object behind.
func TestCanceledPutLeavesNoCommittedObject(t *testing.T) {
	t.Run("canceled before Put starts", func(t *testing.T) {
		server, s, w := newAnswered(t, t.TempDir(), nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := s.Put(ctx, strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Put = %v, want context.Canceled", err)
		}
		if _, uploads := w.counts(); uploads != 0 {
			t.Fatalf("%d upload requests after cancellation, want 0", uploads)
		}
		if names := objectNames(t, server); len(names) != 0 {
			t.Fatalf("bucket holds %v after a canceled Put, want nothing", names)
		}
	})

	t.Run("canceled as the upload request is issued", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		answer := func(req *http.Request) *http.Response {
			if isCreateUpload(req) {
				cancel()
			}
			return nil
		}
		server, s, _ := newAnswered(t, t.TempDir(), answer)

		_, err := s.Put(ctx, strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Put = %v, want context.Canceled", err)
		}
		if names := objectNames(t, server); len(names) != 0 {
			t.Fatalf("bucket holds %v after a canceled upload, want nothing", names)
		}
	})
}

// Construction refuses a client that is nil and an endpoint that is not an
// absolute http(s) URL with a host, before any request is made.
func TestConstructionRefusesNilClientAndBadEndpoints(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		if _, err := gcs.NewFromClient(nil, gcs.Config{Bucket: testBucket}); err == nil {
			t.Fatal("NewFromClient(nil) succeeded, want an error")
		}
	})

	endpoints := map[string]string{
		"no host":          "http://",
		"unsupported":      "ftp://storage.example",
		"unparseable":      "http://[::1",
		"relative":         "/storage/v1/",
		"scheme-less host": "storage.example",
	}
	for name, endpoint := range endpoints {
		t.Run(name, func(t *testing.T) {
			if _, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: endpoint}); err == nil {
				t.Fatalf("New with endpoint %q succeeded, want an error", endpoint)
			}
		})
	}
}

// RedactCredentials removes every credential string value from a message, in
// its decoded and JSON-escaped forms, at any depth including arrays. Values under
// four bytes and numbers are left alone, and malformed credentials are replaced
// by a fixed message.
func TestRedactCredentialsRemovesEveryFormOfEachStringValue(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		raw  string
		want string
	}{
		{
			name: "decoded value is replaced",
			msg:  "rejected key arraysecret-1 for project",
			raw:  `{"private_key":"arraysecret-1"}`,
			want: "rejected key [redacted] for project",
		},
		{
			name: "values inside an array are replaced",
			msg:  "x arraysecret-1 y arraysecret-2",
			raw:  `{"keys":["arraysecret-1","arraysecret-2"]}`,
			want: "x [redacted] y [redacted]",
		},
		{
			name: "JSON-escaped form is replaced",
			msg:  `err: quote\"and<angle>`,
			raw:  `{"k":"quote\"and<angle>"}`,
			want: "err: [redacted]",
		},
		{
			name: "value under four bytes is kept",
			msg:  "id abc here",
			raw:  `{"k":"abc"}`,
			want: "id abc here",
		},
		{
			name: "numbers carry no secret and are kept",
			msg:  "id 12345678 here",
			raw:  `{"n":12345678}`,
			want: "id 12345678 here",
		},
		{
			name: "malformed credentials get a fixed message",
			msg:  "anything at all",
			raw:  `{"k":"unterminated`,
			want: "storage/gcs: client: credentials are not valid JSON",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := gcs.RedactCredentials(tc.msg, []byte(tc.raw))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("RedactCredentials = %v, want %q", err, tc.want)
			}
		})
	}
}

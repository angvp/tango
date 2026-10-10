package s3_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/s3"
	"github.com/angvp/tango/storagetest"
)

const testBucket = "uploads"

var staticKeys = &s3.Credentials{AccessKeyID: "AKIATESTONLY", SecretAccessKey: "not-a-real-secret"}

// isolateAWS keeps the SDK from reading the developer's own AWS setup.
func isolateAWS(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", os.DevNull)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", os.DevNull)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
}

func newStore(t *testing.T, endpoint, prefix string, mutate func(*s3.Config)) *s3.Store {
	t.Helper()
	isolateAWS(t)
	cfg := s3.Config{Bucket: testBucket, Region: "us-east-1", Endpoint: endpoint, PathStyle: true, Prefix: prefix, Credentials: staticKeys, TempDir: t.TempDir(), PartSize: s3.MinPartSize}
	if mutate != nil {
		mutate(&cfg)
	}
	s, err := s3.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3PassesTheConformanceSuiteAgainstTheFake(t *testing.T) {
	for name, prefix := range map[string]string{"no prefix": "", "a prefix": "app/uploads"} {
		t.Run(name, func(t *testing.T) {
			_, server := newFakeS3(t, testBucket)
			storagetest.Run(t, storagetest.Factory{
				New: func(t *testing.T) storage.Store { return newStore(t, server.URL, prefix, nil) },
				NewWithKeys: func(t *testing.T, next func() string) storage.Store {
					s := newStore(t, server.URL, prefix, nil)
					s.SetKeyFunc(next)
					return s
				},
			})
		})
	}
}

// TestS3PassesTheConformanceSuiteAgainstARealBucket runs the suite against any
// S3-compatible bucket named by the environment, and is skipped otherwise.
// Credentials come from TANGO_TEST_S3_ACCESS_KEY_ID and
// TANGO_TEST_S3_SECRET_ACCESS_KEY, or the SDK's standard chain when unset.
// The suite leaves its objects behind (a Store cannot list), so point it at
// a bucket with an expiry rule.
func TestS3PassesTheConformanceSuiteAgainstARealBucket(t *testing.T) {
	endpoint := os.Getenv("TANGO_TEST_S3_ENDPOINT")
	bucket := os.Getenv("TANGO_TEST_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("set TANGO_TEST_S3_ENDPOINT and TANGO_TEST_S3_BUCKET to run against a real bucket")
	}
	build := func(t *testing.T) *s3.Store {
		cfg := s3.Config{
			Bucket: bucket, Region: os.Getenv("TANGO_TEST_S3_REGION"), Endpoint: endpoint,
			PathStyle: os.Getenv("TANGO_TEST_S3_PATH_STYLE") != "", Prefix: "tango-conformance", TempDir: t.TempDir(),
		}
		if id := os.Getenv("TANGO_TEST_S3_ACCESS_KEY_ID"); id != "" {
			cfg.Credentials = &s3.Credentials{AccessKeyID: id, SecretAccessKey: os.Getenv("TANGO_TEST_S3_SECRET_ACCESS_KEY")}
		}
		if cfg.Region == "" {
			cfg.Region = "auto"
		}
		s, err := s3.New(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	storagetest.Run(t, storagetest.Factory{
		New: func(t *testing.T) storage.Store { return build(t) },
		NewWithKeys: func(t *testing.T, next func() string) storage.Store {
			s := build(t)
			s.SetKeyFunc(next)
			return s
		},
	})
}

func put(t *testing.T, s *s3.Store, data []byte) storage.Object {
	t.Helper()
	obj, err := s.Put(context.Background(), bytes.NewReader(data), storage.PutOptions{MaxSize: 64 << 20})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return obj
}

var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

func TestAnObjectIsStoredUnderThePrefixWithTheSniffedTypeAndItsHash(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "app/files/", nil)
	data := append(append([]byte{}, pngHead...), bytes.Repeat([]byte("p"), 1000)...)
	obj := put(t, s, data)

	stored, ok := fake.object("app/files/" + obj.Key)
	if !ok {
		t.Fatalf("no object named app/files/%s", obj.Key)
	}
	sum := sha256.Sum256(data)
	if got := stored.headers.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want the sniffed image/png", got)
	}
	if got := stored.headers.Get("X-Amz-Meta-Sha256"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 metadata = %q", got)
	}
	if fake.conditionalPuts != 1 {
		t.Errorf("conditional puts = %d, want the create-only If-None-Match on the one write", fake.conditionalPuts)
	}
}

func TestALargeObjectIsUploadedInPartsAndReadBackWhole(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	data := bytes.Repeat([]byte("0123456789abcdef"), (11<<20)/16) // 11 MiB: three 5 MiB parts
	obj := put(t, s, data)

	if fake.multipartStarted != 1 {
		t.Fatalf("multipart uploads started = %d, want 1", fake.multipartStarted)
	}
	if _, uploads, _ := fake.count(); uploads != 0 {
		t.Fatalf("%d multipart uploads left open", uploads)
	}
	sum := sha256.Sum256(data)
	info, err := s.Stat(context.Background(), obj.Key)
	if err != nil || info.Size != int64(len(data)) || info.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("Stat = %+v, %v", info, err)
	}
	rc, err := s.Open(context.Background(), obj.Key, 5<<20-4, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data[5<<20-4:5<<20+4]) {
		t.Fatalf("range across a part boundary = %q", got)
	}
}

func TestAFailedPartAbortsTheUploadAndStoresNothing(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	fake.failPart = 2
	s := newStore(t, server.URL, "", nil)
	_, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("z"), 11<<20)), storage.PutOptions{MaxSize: 64 << 20})
	if err == nil || errors.Is(err, storage.ErrNotFound) || !strings.Contains(err.Error(), "storage/s3") {
		t.Fatalf("err = %v, want a wrapped storage/s3 error", err)
	}
	if objects, uploads, _ := fake.count(); objects != 0 || uploads != 0 {
		t.Fatalf("%d objects and %d open uploads left behind", objects, uploads)
	}
}

func TestACollisionOnALargeObjectNeverOverwrites(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	key := storage.NewKey()
	s.SetKeyFunc(func() string { return key })
	first := bytes.Repeat([]byte("a"), 6<<20)
	put(t, s, first)
	_, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("b"), 6<<20)), storage.PutOptions{MaxSize: 64 << 20})
	if !errors.Is(err, storage.ErrExists) {
		t.Fatalf("second Put: %v, want ErrExists", err)
	}
	if stored, _ := fake.object(key); !bytes.Equal(stored.data, first) {
		t.Fatal("the first object was overwritten")
	}
	if _, uploads, _ := fake.count(); uploads != 0 {
		t.Fatalf("%d multipart uploads left open", uploads)
	}
}

func TestAnObjectThatWasNotWrittenHereIsNotFound(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	key := storage.NewKey()
	fake.seed(key, []byte("foreign"))
	if _, err := s.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat: %v, want ErrNotFound", err)
	}
	if _, err := s.Open(context.Background(), key, 0, -1); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Open: %v, want ErrNotFound", err)
	}
}

func TestAnInvalidKeyNeverReachesTheNetwork(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "app", nil)
	for _, key := range []string{"../app/secret", "", "a/b", strings.Repeat("a", 33)} {
		if _, err := s.Stat(context.Background(), key); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Stat(%q): %v", key, err)
		}
		if err := s.Delete(context.Background(), key); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Delete(%q): %v", key, err)
		}
	}
	if _, _, requests := fake.count(); requests != 0 {
		t.Fatalf("%d requests were sent for invalid keys", requests)
	}
}

func TestAProviderSizeRefusalIsErrTooLarge(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	fake.maxObject = 500
	s := newStore(t, server.URL, "", nil)
	_, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 2000)), storage.PutOptions{MaxSize: 4096})
	var tooLarge *storage.TooLargeError
	if !errors.Is(err, storage.ErrTooLarge) || !errors.As(err, &tooLarge) || tooLarge.Max != 4096 {
		t.Fatalf("err = %v, want ErrTooLarge with Max 4096", err)
	}
}

func TestOtherProviderErrorsAreWrappedNotHidden(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	fake.denyHead = true
	s := newStore(t, server.URL, "", nil)
	_, err := s.Stat(context.Background(), storage.NewKey())
	if err == nil || errors.Is(err, storage.ErrNotFound) || !strings.HasPrefix(err.Error(), "storage/s3:") {
		t.Fatalf("err = %v, want a wrapped storage/s3 error that is not ErrNotFound", err)
	}
}

func TestConfigIsValidatedBeforeAnyRequest(t *testing.T) {
	isolateAWS(t)
	tests := []struct {
		name string
		cfg  s3.Config
	}{
		{"no bucket", s3.Config{Region: "us-east-1"}},
		{"a relative endpoint", s3.Config{Bucket: "b", Region: "us-east-1", Endpoint: "localhost:9000"}},
		{"an ftp endpoint", s3.Config{Bucket: "b", Region: "us-east-1", Endpoint: "ftp://example.com"}},
		{"a parent segment in the prefix", s3.Config{Bucket: "b", Region: "us-east-1", Prefix: "a/../b"}},
		{"a dot segment in the prefix", s3.Config{Bucket: "b", Region: "us-east-1", Prefix: "./a"}},
		{"a leading slash in the prefix", s3.Config{Bucket: "b", Region: "us-east-1", Prefix: "/a"}},
		{"a doubled slash in the prefix", s3.Config{Bucket: "b", Region: "us-east-1", Prefix: "a//b"}},
		{"a backslash in the prefix", s3.Config{Bucket: "b", Region: "us-east-1", Prefix: `a\b`}},
		{"a part size below S3's minimum", s3.Config{Bucket: "b", Region: "us-east-1", PartSize: 1 << 20}},
		{"no region anywhere", s3.Config{Bucket: "b", Credentials: staticKeys}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s3.New(context.Background(), tt.cfg); err == nil {
				t.Fatal("New succeeded, want an error")
			}
		})
	}
	if _, err := s3.New(context.Background(), s3.Config{Bucket: "b", Region: "auto", Endpoint: "https://acct.r2.cloudflarestorage.com", Prefix: "a/b/", Credentials: staticKeys}); err != nil {
		t.Fatalf("a valid R2-style configuration: %v", err)
	}
}

package gcs_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gcsstorage "cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
)

const secretMarker = "SECRET-CREDENTIAL-MARKER-91c2"

// testKey is a well-formed object key; Stat refuses any other before a request.
const testKey = "abcdefghijklmnopqrstuvwxyz234567"

// isolateAmbientCredentials hides every credential source a developer machine
// may have, so a test sees exactly the ones it sets.
func isolateAmbientCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	t.Setenv("GCE_METADATA_HOST", "")
	t.Setenv("STORAGE_EMULATOR_HOST", "")
}

func TestChunkSizeMustBeAPositiveMultipleOf256KiBAndIsRefusedBeforeAnyRequest(t *testing.T) {
	for name, size := range map[string]int64{
		"one byte":            1,
		"just over a quantum": 256<<10 + 1,
		"negative":            -(256 << 10),
	} {
		t.Run(name, func(t *testing.T) {
			rec := newRecorder(t, http.StatusOK, "{}")
			_, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: rec.srv.URL, ChunkSize: size, TempDir: t.TempDir()})
			if err == nil {
				t.Fatalf("New accepted ChunkSize %d", size)
			}
			if n := len(rec.seen()); n != 0 {
				t.Fatalf("refused configuration still sent %d requests", n)
			}
		})
	}
}

func TestChunkSizeIsTheWritersChunkSize(t *testing.T) {
	cases := []struct {
		name       string
		chunk      int64
		content    int
		wantChunks int
	}{
		{name: "explicit 256 KiB", chunk: 256 << 10, content: 1<<20 + 1, wantChunks: 5},
		{name: "default 8 MiB", chunk: 0, content: 9 << 20, wantChunks: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := newFake(t)
			w := &wire{base: http.DefaultTransport}
			s, err := gcs.NewWithHTTPClient(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), ChunkSize: c.chunk, TempDir: t.TempDir()}, &http.Client{Transport: w})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Put(context.Background(), bytes.NewReader(make([]byte, c.content)), storage.PutOptions{MaxSize: 16 << 20}); err != nil {
				t.Fatalf("Put: %v", err)
			}
			if got := w.counts().chunks; got != c.wantChunks {
				t.Fatalf("upload sent %d chunks, want %d", got, c.wantChunks)
			}
		})
	}
}

func TestCredentialOptionsAreExclusiveAndRefusedBeforeAnyRequest(t *testing.T) {
	isolateAmbientCredentials(t)
	rec := newRecorder(t, http.StatusOK, "{}")
	json := []byte(`{"type":"` + secretMarker + `"}`)
	cases := map[string]gcs.Config{
		"file and JSON":     {Bucket: testBucket, CredentialsFile: "/nonexistent/key.json", CredentialsJSON: json},
		"endpoint and file": {Bucket: testBucket, Endpoint: rec.srv.URL, CredentialsFile: "/nonexistent/key.json"},
		"endpoint and JSON": {Bucket: testBucket, Endpoint: rec.srv.URL, CredentialsJSON: json},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := gcs.New(context.Background(), cfg)
			if err == nil {
				t.Fatal("New accepted conflicting credential options")
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Fatalf("constructor error echoes a credential: %v", err)
			}
			if n := len(rec.seen()); n != 0 {
				t.Fatalf("refused configuration still sent %d requests", n)
			}
		})
	}
}

func TestNewDoesNotLookUpCredentials(t *testing.T) {
	isolateAmbientCredentials(t)
	_, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New with no credentials available failed at construction: %v", err)
	}
}

func TestApplicationDefaultCredentialsAreUsedAndTheEmulatorVariableIsNotHonoured(t *testing.T) {
	isolateAmbientCredentials(t)
	metadata := newRecorder(t, http.StatusOK, `{"access_token":"token","expires_in":3600,"token_type":"Bearer"}`)
	emulator := newRecorder(t, http.StatusNotFound, `{"error":{"code":404,"message":"not found"}}`)
	google := newRecorder(t, http.StatusNotFound, `{"error":{"code":404,"message":"not found"}}`)
	t.Setenv("GCE_METADATA_HOST", metadata.hostPort())
	t.Setenv("STORAGE_EMULATOR_HOST", emulator.hostPort())
	restore := gcs.SetGoogleEndpoint(google.srv.URL + "/storage/v1/")
	t.Cleanup(restore)

	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Stat(ctx, testKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat error = %v, want ErrNotFound from the Google endpoint", err)
	}

	if len(metadata.seen()) == 0 {
		t.Fatal("no token was fetched: the request did not use application-default credentials")
	}
	if got := google.seen(); len(got) != 1 || got[0] != "GET /storage/v1/b/"+testBucket+"/o/"+testKey {
		t.Fatalf("Google endpoint received %v, want one object read", got)
	}
	if got := google.authorizations(); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("Google endpoint received Authorization %v, want the application-default token", got)
	}
	if n := len(emulator.seen()); n != 0 {
		t.Fatalf("STORAGE_EMULATOR_HOST received %d requests", n)
	}
}

func TestEndpointIgnoresTheEmulatorVariable(t *testing.T) {
	isolateAmbientCredentials(t)
	emulator := newRecorder(t, http.StatusNotFound, `{"error":{"code":404,"message":"not found"}}`)
	t.Setenv("STORAGE_EMULATOR_HOST", emulator.hostPort())
	server := newFake(t)
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), bytes.NewReader([]byte("hello")), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n := len(emulator.seen()); n != 0 {
		t.Fatalf("STORAGE_EMULATOR_HOST received %d requests", n)
	}
}

func TestCredentialErrorsNeverEchoTheSecret(t *testing.T) {
	isolateAmbientCredentials(t)
	dir := t.TempDir()
	fileWithSecret := filepath.Join(dir, "key.json")
	if err := os.WriteFile(fileWithSecret, []byte(`{"type":"`+secretMarker+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]gcs.Config{
		"type echoed by the SDK":  {Bucket: testBucket, CredentialsJSON: []byte(`{"type":"` + secretMarker + `"}`)},
		"malformed JSON":          {Bucket: testBucket, CredentialsJSON: []byte(`{"type":"service_account","private_key":"` + secretMarker + `"`)},
		"type echoed from a file": {Bucket: testBucket, CredentialsFile: fileWithSecret},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			cfg.TempDir = t.TempDir()
			s, err := gcs.New(context.Background(), cfg)
			if err != nil && strings.Contains(err.Error(), secretMarker) {
				t.Fatalf("constructor error echoes a credential: %v", err)
			}
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err = s.Stat(ctx, testKey)
			if err == nil {
				t.Fatal("Stat with unusable credentials succeeded")
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Fatalf("first-use error echoes a credential: %v", err)
			}
		})
	}
}

// storageClient builds an SDK client for the fake the way a host would.
func storageClient(t *testing.T, endpoint string) *gcsstorage.Client {
	t.Helper()
	c, err := gcsstorage.NewClient(context.Background(), option.WithEndpoint(strings.TrimSuffix(endpoint, "/")+"/storage/v1/"), option.WithoutAuthentication(), gcsstorage.WithJSONReads())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestEmulatorVariableDoesNotRedirectAHostBuiltClient(t *testing.T) {
	emulator := newRecorder(t, http.StatusNotFound, `{"error":{"code":404,"message":"not found"}}`)
	t.Setenv("STORAGE_EMULATOR_HOST", emulator.hostPort())
	server := newFake(t)
	s, err := gcs.NewFromClient(storageClient(t, server.URL()), gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), bytes.NewReader([]byte("hello")), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n := len(emulator.seen()); n != 0 {
		t.Fatalf("STORAGE_EMULATOR_HOST received %d requests", n)
	}
}

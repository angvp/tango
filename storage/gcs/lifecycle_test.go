package gcs_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
	"github.com/angvp/tango/storagetest"
)

func TestNewFromClientRefusesEndpointAndCredentialsAndValidatesTheRest(t *testing.T) {
	server := newFake(t)
	client := storageClient(t, server.URL())
	cases := map[string]gcs.Config{
		"empty bucket":     {Prefix: "a"},
		"bad prefix":       {Bucket: testBucket, Prefix: "a/../b"},
		"bad chunk size":   {Bucket: testBucket, ChunkSize: 1},
		"endpoint":         {Bucket: testBucket, Endpoint: server.URL()},
		"credentials file": {Bucket: testBucket, CredentialsFile: "/nonexistent/key.json"},
		"credentials JSON": {Bucket: testBucket, CredentialsJSON: []byte(`{"type":"` + secretMarker + `"}`)},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := gcs.NewFromClient(client, cfg)
			if err == nil {
				t.Fatal("NewFromClient accepted the configuration")
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Fatalf("error echoes a credential: %v", err)
			}
		})
	}
}

func TestNewFromClientConformsToTheSuiteAgainstTheFake(t *testing.T) {
	server := newFake(t)
	client := storageClient(t, server.URL())
	storagetest.Run(t, storagetest.Factory{
		New: func(t *testing.T) storage.Store {
			s, err := gcs.NewFromClient(client, gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
		NewWithKeys: func(t *testing.T, next func() string) storage.Store {
			s, err := gcs.NewFromClient(client, gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			s.SetKeyFunc(next)
			return s
		},
	})
}

func TestCloseLeavesAnInjectedClientUsable(t *testing.T) {
	server := newFake(t)
	s, err := gcs.NewFromClient(storageClient(t, server.URL()), gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.Put(context.Background(), bytes.NewReader([]byte("after close")), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("Put after Close on an injected store: %v", err)
	}
}

func TestCloseEndsUseOfAnAdapterClientWithoutPanicking(t *testing.T) {
	server := newFake(t)
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), bytes.NewReader([]byte("before close")), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := s.Stat(context.Background(), testKey); err == nil {
		t.Fatal("Stat after Close succeeded on an adapter-created client")
	}
}

func TestAnUnclassifiedFailureOfAnInjectedClientKeepsItsCause(t *testing.T) {
	const marker = "injected-marker-403"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"` + marker + `"}}`))
	}))
	t.Cleanup(server.Close)
	s, err := gcs.NewFromClient(storageClient(t, server.URL), gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Stat(context.Background(), testKey)
	if err == nil {
		t.Fatal("Stat succeeded against a server that refuses everything")
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("the SDK error was not kept in the chain: %v", err)
	}
	if !strings.Contains(err.Error(), marker) {
		t.Fatalf("error lost the server's failure: %v", err)
	}
}

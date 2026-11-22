package gcs_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
)

var _ storage.PublicURLer = (*gcs.Store)(nil)

func newURLStore(t *testing.T, publicURL, prefix string) *gcs.Store {
	t.Helper()
	store, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Prefix: prefix, PublicURL: publicURL, TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestPublicURLJoinsTheBaseThePrefixAndTheKey(t *testing.T) {
	key := storage.NewKey()

	got, ok := newURLStore(t, "https://static.example.com", "clients").PublicURL(key)

	if want := "https://static.example.com/clients/" + key; !ok || got != want {
		t.Fatalf("PublicURL = %q, %v, want %q", got, ok, want)
	}
}

func TestPublicURLIsAbsentWithoutABase(t *testing.T) {
	if got, ok := newURLStore(t, "", "").PublicURL(storage.NewKey()); ok || got != "" {
		t.Fatalf("PublicURL = %q, %v, want \"\", false", got, ok)
	}
}

func TestPublicURLRefusesAnInvalidKeyAndAnInvalidBase(t *testing.T) {
	if got, ok := newURLStore(t, "https://static.example.com", "").PublicURL("../secret"); ok || got != "" {
		t.Fatalf("PublicURL of an invalid key = %q, %v, want \"\", false", got, ok)
	}
	if _, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, PublicURL: "?x=1", TempDir: t.TempDir()}); err == nil {
		t.Fatal("New accepted a PublicURL that is not an absolute URL")
	}
}

package s3_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/s3"
)

var _ storage.PublicURLer = (*s3.Store)(nil)

func newURLStore(t *testing.T, publicURL, prefix string) *s3.Store {
	t.Helper()
	store, err := s3.New(context.Background(), s3.Config{
		Bucket: "b", Region: "auto", Endpoint: "https://acct.r2.cloudflarestorage.com",
		Prefix: prefix, PublicURL: publicURL, Credentials: staticKeys,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestPublicURLJoinsTheBaseThePrefixAndTheKey(t *testing.T) {
	key := storage.NewKey()

	got, ok := newURLStore(t, "https://cdn.example.com/", "clients/logos").PublicURL(key)

	if want := "https://cdn.example.com/clients/logos/" + key; !ok || got != want {
		t.Fatalf("PublicURL = %q, %v, want %q", got, ok, want)
	}
}

func TestPublicURLIsAbsentWithoutABase(t *testing.T) {
	if got, ok := newURLStore(t, "", "").PublicURL(storage.NewKey()); ok || got != "" {
		t.Fatalf("PublicURL = %q, %v, want \"\", false", got, ok)
	}
}

func TestPublicURLRefusesAnInvalidKeyAndAnInvalidBase(t *testing.T) {
	if got, ok := newURLStore(t, "https://cdn.example.com", "").PublicURL("../secret"); ok || got != "" {
		t.Fatalf("PublicURL of an invalid key = %q, %v, want \"\", false", got, ok)
	}
	_, err := s3.New(context.Background(), s3.Config{Bucket: "b", Region: "auto", PublicURL: "cdn.example.com", Credentials: staticKeys})
	if err == nil {
		t.Fatal("New accepted a PublicURL with no scheme")
	}
}

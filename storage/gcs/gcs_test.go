package gcs_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
	"github.com/angvp/tango/storagetest"
)

func newStore(t *testing.T, endpoint, prefix string) *gcs.Store {
	t.Helper()
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: endpoint, Prefix: prefix, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGCSPassesTheConformanceSuiteAgainstTheFake(t *testing.T) {
	for name, prefix := range map[string]string{"no prefix": "", "a prefix": "app/uploads"} {
		t.Run(name, func(t *testing.T) {
			server := newFake(t)
			storagetest.Run(t, storagetest.Factory{
				New: func(t *testing.T) storage.Store { return newStore(t, server.URL(), prefix) },
				NewWithKeys: func(t *testing.T, next func() string) storage.Store {
					s := newStore(t, server.URL(), prefix)
					s.SetKeyFunc(next)
					return s
				},
			})
		})
	}
}

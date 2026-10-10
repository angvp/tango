package storagetest_test

import (
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storagetest"
)

func TestMemoryPassesTheConformanceSuite(t *testing.T) {
	storagetest.Run(t, storagetest.Factory{
		New:         func(*testing.T) storage.Store { return storagetest.NewMemory() },
		NewWithKeys: func(_ *testing.T, next func() string) storage.Store { return storagetest.NewMemoryWithKeys(next) },
	})
}

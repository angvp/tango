package cachetest_test

import (
	"testing"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/local"
	"github.com/angvp/tango/cachetest"
)

// The suite is shared by every adapter, so it is run here against the
// in-process store: a change to the suite is exercised by `go test` of this
// package, without a Redis or Memcached server.
func TestTheSuitePassesForTheLocalStore(t *testing.T) {
	cachetest.Run(t, cachetest.Factory{New: func(t *testing.T) cache.Store {
		store, err := local.New(local.Config{MaxEntries: 10000})
		if err != nil {
			t.Fatal(err)
		}
		return store
	}})
}

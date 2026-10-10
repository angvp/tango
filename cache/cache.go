// Package cache is a small, explicit cache for expensive reads that are safe
// to recompute. A [Store] holds bytes under a key for a bounded time; the
// code that uses it receives one and calls it where it wants caching. There
// is no ambient configuration, no automatic caching of database queries and
// no invalidation from writes: a cache only ever makes a read cheaper, and
// the database stays the source of truth.
//
// A miss is not an error: Get reports it as ok == false with a nil error, and
// an error always means the backend failed, so a hit, a miss and a failure
// are three different outcomes. The Store, the stores in cache/local,
// cache/redis and cache/memcache, and the helpers here never log; they return
// errors and the host decides what is worth recording.
//
// Keys follow one rule every backend can store (see [ValidKey]), and a TTL
// has one portable range (see [CheckTTL]). Values are bytes, whose format is
// the host's. The conformance suite in package cachetest holds every Store to
// this contract.
package cache

import (
	"context"
	"time"
)

// Store is a cache. Implementations are safe for concurrent use and pass the
// suite in package cachetest.
type Store interface {
	// Get returns the value stored under key. A miss, including an expired
	// value, is ok == false with a nil error; an error means the backend
	// failed. The returned slice belongs to the caller.
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)
	// Set stores value under key for ttl, replacing any existing value. The
	// Store does not retain value after Set returns. It fails with
	// ErrInvalidKey, or ErrInvalidTTL unless 0 < ttl <= MaxTTL, before any
	// request, and with ErrTooLarge for a value the backend refuses for size.
	// A value never expires sooner than ttl.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes key. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
}

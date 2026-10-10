package memcache

import (
	"time"
)

// ExpirySeconds exposes the TTL conversion to the tests.
func ExpirySeconds(ttl time.Duration) int32 { return expirySeconds(ttl) }

// HasTLSDialer reports whether a Store dials through a TLS dialer.
func (s *Store) HasTLSDialer() bool { return s.client.DialContext != nil }

// IsTooLarge exposes the too-large classification to the tests.
func IsTooLarge(err error) bool { return isTooLarge(err) }

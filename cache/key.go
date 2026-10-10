package cache

import (
	"fmt"
	"time"
)

const (
	// MaxKeyLength is the longest key, in bytes. It is below Memcached's 250
	// byte limit so a Prefix has room to grow.
	MaxKeyLength = 200
	// MaxTTL is the longest portable TTL. A longer retention belongs in your
	// database or object storage, not in a cache.
	MaxTTL = 30 * 24 * time.Hour
)

// ValidKey reports whether key is 1 to MaxKeyLength bytes of printable ASCII
// with no spaces, which every backend can store.
func ValidKey(key string) bool {
	if len(key) == 0 || len(key) > MaxKeyLength {
		return false
	}
	for i := 0; i < len(key); i++ {
		if c := key[i]; c <= ' ' || c >= 0x7f {
			return false
		}
	}
	return true
}

// CheckTTL returns ErrInvalidTTL unless 0 < ttl <= MaxTTL.
func CheckTTL(ttl time.Duration) error {
	if ttl <= 0 || ttl > MaxTTL {
		return fmt.Errorf("%w: %v is outside (0, %v]", ErrInvalidTTL, ttl, MaxTTL)
	}
	return nil
}

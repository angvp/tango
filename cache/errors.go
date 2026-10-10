package cache

import "errors"

var (
	// ErrInvalidKey is returned for a key that is not 1 to 200 bytes of
	// printable ASCII without spaces, before any request is made.
	ErrInvalidKey = errors.New("cache: invalid key")
	// ErrInvalidTTL is returned for a TTL that is not greater than zero and
	// at most MaxTTL, before any request is made.
	ErrInvalidTTL = errors.New("cache: invalid ttl")
	// ErrTooLarge is returned when a Store refuses a value for its size.
	ErrTooLarge = errors.New("cache: value is too large")
)

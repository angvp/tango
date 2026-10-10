package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GetJSON reads the value under key and decodes it as JSON into a T. A miss
// is ok == false with a nil error. Bytes that do not decode into T are an
// error matching ErrCorrupt, never a zero-value hit. Any other error is the
// Store's own.
func GetJSON[T any](ctx context.Context, store Store, key string) (T, bool, error) {
	var zero T
	raw, ok, err := store.Get(ctx, key)
	if err != nil || !ok {
		return zero, false, err
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false, &CorruptError{Key: key, Err: err}
	}
	return value, true, nil
}

// SetJSON encodes value as JSON and stores it under key for ttl. The encoding
// is plain encoding/json: no codec is configurable, and the stored bytes are
// yours to read with any other tool.
func SetJSON[T any](ctx context.Context, store Store, key string, value T, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return &encodeError{key: key, err: err}
	}
	return store.Set(ctx, key, raw, ttl)
}

// encodeError marks a value SetJSON could not encode, so FetchJSON can tell
// it from a Store failure.
type encodeError struct {
	key string
	err error
}

func (e *encodeError) Error() string {
	return fmt.Sprintf("cache: value for %q cannot be encoded: %v", e.key, e.err)
}

func (e *encodeError) Unwrap() error { return e.err }

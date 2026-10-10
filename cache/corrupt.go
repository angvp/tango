package cache

import (
	"errors"
	"fmt"
)

// ErrCorrupt is returned by GetJSON for a stored value that cannot be decoded
// into the requested type. Match it with errors.Is; errors.As with
// *CorruptError gives the key and the decode error.
var ErrCorrupt = errors.New("cache: corrupt value")

// CorruptError is the typed form of ErrCorrupt.
type CorruptError struct {
	// Key is the key whose value could not be decoded.
	Key string
	// Err is the underlying decode error.
	Err error
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("cache: value under %q cannot be decoded: %v", e.Key, e.Err)
}

// Unwrap returns the underlying decode error.
func (e *CorruptError) Unwrap() error { return e.Err }

// Is matches ErrCorrupt.
func (e *CorruptError) Is(target error) bool { return target == ErrCorrupt }

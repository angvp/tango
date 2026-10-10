package storage

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned for a key with no object.
	ErrNotFound = errors.New("storage: object not found")
	// ErrExists is returned when a create-only write meets an existing key.
	// With generated keys this is a collision and is retried before it
	// reaches the caller.
	ErrExists = errors.New("storage: object already exists")
	// ErrTooLarge is returned for bytes past PutOptions.MaxSize. Match the
	// limit with errors.As and *TooLargeError.
	ErrTooLarge = errors.New("storage: object is too large")
	// ErrTypeNotAllowed is returned for a sniffed type outside
	// PutOptions.AllowedTypes. Match the type with errors.As and
	// *TypeNotAllowedError.
	ErrTypeNotAllowed = errors.New("storage: content type is not allowed")
	// ErrInvalidKey is returned for any key that is not in the form NewKey
	// produces.
	ErrInvalidKey = errors.New("storage: invalid key")
	// ErrInvalidRange is returned for an Open offset that is negative or
	// beyond the end of the object.
	ErrInvalidRange = errors.New("storage: invalid range")
	// ErrInvalidOptions is returned for PutOptions that cannot be honoured,
	// such as a MaxSize that is not positive.
	ErrInvalidOptions = errors.New("storage: invalid options")
)

// TooLargeError carries the limit a write exceeded. It matches ErrTooLarge.
type TooLargeError struct{ Max int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("storage: object is larger than %d bytes", e.Max)
}

func (e *TooLargeError) Is(target error) bool { return target == ErrTooLarge }

// TypeNotAllowedError carries the type that was sniffed. It matches
// ErrTypeNotAllowed.
type TypeNotAllowedError struct{ Type string }

func (e *TypeNotAllowedError) Error() string {
	return fmt.Sprintf("storage: content type %q is not allowed", e.Type)
}

func (e *TypeNotAllowedError) Is(target error) bool { return target == ErrTypeNotAllowed }

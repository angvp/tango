// Package storage keeps uploaded bytes outside the database. A [Store] is a
// small object store: Put streams bytes in and returns a generated key,
// Stat describes an object, Open reads a range of it and Delete removes it.
// There is no listing and no model field: the host keeps the key and the
// metadata in its own model, authorizes every access in its own View, and
// can use [Serve] to deliver an object it has authorized.
//
// A Store never logs; it returns typed errors the host matches with
// errors.Is. Keys are always generated here (see [NewKey]), never derived
// from a client filename.
package storage

import (
	"context"
	"io"
	"time"
)

// Store is an object store. Implementations are safe for concurrent use and
// pass the conformance suite in package storagetest.
type Store interface {
	// Put streams r into a new object and returns its generated key. The
	// write is atomic (the object is fully visible or absent) and
	// create-only. It always sniffs the content type from the stream, and
	// fails with ErrTooLarge past opts.MaxSize or ErrTypeNotAllowed outside
	// opts.AllowedTypes, leaving nothing stored.
	Put(ctx context.Context, r io.Reader, opts PutOptions) (Object, error)
	// Stat describes the object at key, or fails with ErrNotFound.
	Stat(ctx context.Context, key string) (Info, error)
	// Open reads length bytes of the object at key from offset; a negative
	// length, or one that reaches or exceeds the remaining bytes (math.MaxInt64
	// included), reads to the end. An
	// offset beyond the end fails with ErrInvalidRange. The caller closes
	// the reader.
	Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	// Delete removes the object at key. Deleting a missing object is not an
	// error.
	Delete(ctx context.Context, key string) error
}

// PutOptions are the host's rules for one write.
type PutOptions struct {
	// MaxSize is the most bytes the object may hold. It must be positive.
	MaxSize int64
	// AllowedTypes, when not empty, lists the sniffed media types accepted:
	// "image/png", or "image/*" for any subtype. Parameters and case are
	// ignored. Nothing a client declares is consulted.
	AllowedTypes []string
}

// Object is what a successful Put stored. The host copies it into its own
// metadata model.
type Object struct {
	// Key is the generated, opaque key. Keep it; it is the only handle.
	Key string
	// Size is the number of bytes stored.
	Size int64
	// SHA256 is the lowercase hex SHA-256 of the bytes.
	SHA256 string
	// ContentType is the type sniffed from the bytes, such as "image/png".
	ContentType string
}

// Info describes a stored object.
type Info struct {
	Key         string
	Size        int64
	SHA256      string
	ContentType string
	// ETag is the quoted validator, derived from the SHA-256.
	ETag    string
	ModTime time.Time
}

// ETagFor returns the quoted ETag for an object whose SHA-256 is sha256hex.
func ETagFor(sha256hex string) string { return `"` + sha256hex + `"` }

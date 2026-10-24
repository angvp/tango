// Package storagekit holds the few helpers every storage.Store
// implementation shares, so the adapters (storage/local, storage/s3 and
// storagetest's memory store) cannot drift apart on them. It is internal:
// an adapter written elsewhere writes its own, and the conformance suite
// holds it to the same contract.
package storagekit

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/angvp/tango/storage"
)

// KeyAttempts is how many generated keys a write tries before it reports a
// collision as storage.ErrExists.
const KeyAttempts = 3

// Check is the preamble every call that names a key shares: the context,
// then the key's form.
func Check(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !storage.ValidKey(key) {
		return storage.ErrInvalidKey
	}
	return nil
}

// ContextReader stops a copy as soon as Ctx is done.
type ContextReader struct {
	Ctx context.Context
	R   io.Reader
}

// Read implements io.Reader.
func (c *ContextReader) Read(p []byte) (int, error) {
	if err := c.Ctx.Err(); err != nil {
		return 0, err
	}
	return c.R.Read(p)
}

// Spool copies r into a new file in dir and returns it, open. prefix names the
// adapter in errors. A failed copy leaves no file behind.
func Spool(ctx context.Context, prefix, dir, pattern string, r io.Reader) (*os.File, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: spool: %w", prefix, err)
	}
	if _, err := io.Copy(f, &ContextReader{Ctx: ctx, R: r}); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("%s: spool: %w", prefix, err)
	}
	return f, nil
}

// ReleaseSpool closes a file from Spool and removes it. A close error is
// returned, since on a written file it can be the first sign that the bytes
// never reached the disk; the removal is best effort.
func ReleaseSpool(prefix string, f *os.File) error {
	closeErr := f.Close()
	_ = os.Remove(f.Name())
	if closeErr != nil {
		return fmt.Errorf("%s: spool close: %w", prefix, closeErr)
	}
	return nil
}

// NormalizePrefix returns prefix as "" or "a/b/" after checking that it cannot
// name anything outside itself. The error names the offending prefix but not
// the adapter, which wraps it.
func NormalizePrefix(prefix string) (string, error) {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return "", nil
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("Config.Prefix %q has an empty, \".\" or \"..\" segment", prefix)
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
				return "", fmt.Errorf("Config.Prefix %q has the character %q", prefix, c)
			}
		}
	}
	return prefix + "/", nil
}

// ValidSHA256 reports whether s is a lowercase hex SHA-256 digest, the form
// the adapters store as object metadata.
func ValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Range resolves an Open request against an object of size bytes: it returns
// the end of the range starting at offset. A negative length, or one that
// reaches or exceeds the remaining bytes (math.MaxInt64 included), reads to
// the end. An offset that is negative or beyond the end is
// storage.ErrInvalidRange. It never overflows, whatever length is.
func Range(size, offset, length int64) (end int64, err error) {
	if offset < 0 || offset > size {
		return 0, storage.ErrInvalidRange
	}
	if length < 0 || length >= size-offset {
		return size, nil
	}
	return offset + length, nil
}

// Package storagekit holds the few helpers every storage.Store
// implementation shares, so the adapters (storage/local, storage/s3 and
// storagetest's memory store) cannot drift apart on them. It is internal:
// an adapter written elsewhere writes its own, and the conformance suite
// holds it to the same contract.
package storagekit

import (
	"context"
	"io"

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

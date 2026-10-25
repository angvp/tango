package gcs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	gcsstorage "cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

// Put implements storage.Store. The upload is create-only and atomic: GCS
// shows an object only once its upload completes.
func (s *Store) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (obj storage.Object, err error) {
	if err := ctx.Err(); err != nil {
		return storage.Object{}, err
	}
	if _, err := s.sdk(ctx); err != nil {
		return storage.Object{}, err
	}
	inspected, err := storage.Inspect(r, opts)
	if err != nil {
		return storage.Object{}, err
	}
	spool, err := storagekit.NewSpoolFile("storage/gcs", s.tempDir, "tango-gcs-*", s.spoolOps)
	if err != nil {
		return storage.Object{}, err
	}
	// Release is best effort and its error is not returned: once the upload
	// has committed, a cleanup failure must not turn it into a failed Put.
	defer func() { _ = s.releaseSpool(spool) }()
	if err := spool.Fill(ctx, inspected); err != nil {
		return storage.Object{}, err
	}
	file, err := spool.Open()
	if err != nil {
		return storage.Object{}, err
	}
	obj = inspected.Object("")
	token, err := s.newToken()
	if err != nil {
		return storage.Object{}, err
	}

	key := s.nextKey()
	for attempt := 0; attempt < storagekit.KeyAttempts; attempt++ {
		if !storage.ValidKey(key) {
			return storage.Object{}, fmt.Errorf("storage/gcs: generated key %q is invalid", key)
		}
		err := s.create(ctx, file, obj, token, key)
		if err == nil {
			obj.Key = key
			return obj, nil
		}
		if !isPrecondition(err) {
			return storage.Object{}, mapError("put", err)
		}
		state, err := s.conflict(ctx, key, obj, token)
		if err != nil {
			return storage.Object{}, err
		}
		switch state {
		case committedHere:
			// An earlier attempt of this same write committed and its
			// response was lost; the SDK's retry then met its own object.
			obj.Key = key
			return obj, nil
		case keyVacant:
			// The object was removed between the conflict and the read, so
			// the key is free: create again under the same key.
		case keyTaken:
			// A different object holds the key. It is left untouched.
			key = s.nextKey()
		}
	}
	return storage.Object{}, storage.ErrExists
}

// conflictState is what a 412 on a create-only write turned out to mean.
type conflictState int

const (
	// committedHere: the object at the key carries this write's SHA-256.
	committedHere conflictState = iota
	// keyTaken: the object at the key is a different write's.
	keyTaken
	// keyVacant: the object was gone when read.
	keyVacant
)

// create sends the spooled bytes as key with the create-only precondition.
// The metadata is set before the first write, because GCS fixes it at the
// start of the upload.
func (s *Store) create(ctx context.Context, spool *os.File, obj storage.Object, token, key string) error {
	handle, err := s.object(ctx, key)
	if err != nil {
		return err
	}
	// The writer's context is cancelled when the copy fails, so that Close
	// aborts the upload instead of finalizing a truncated object. The SDK
	// finalizes on Close unless an error was already recorded, and a failure
	// of the source is not recorded in the writer.
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := handle.If(gcsstorage.Conditions{DoesNotExist: true}).NewWriter(wctx)
	w.ChunkSize = int(s.chunkSize)
	w.ContentType = obj.ContentType
	w.Metadata = map[string]string{shaMetadata: obj.SHA256, tokenMetadata: token}
	_, err = io.Copy(w, spoolSource(spool, obj.Size))
	if err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	return w.Close()
}

// spoolSource is how create reads the spool back. A variable only so a test
// can make the read fail partway.
var spoolSource = func(f *os.File, size int64) io.Reader {
	return io.NewSectionReader(f, 0, size)
}

// conflict reads the object at key once and says what the 412 meant. Only an
// object carrying this write's token, size and SHA-256 is this write's own:
// a SHA-256 alone can be a foreign object's claim over different bytes.
func (s *Store) conflict(ctx context.Context, key string, obj storage.Object, token string) (conflictState, error) {
	handle, err := s.object(ctx, key)
	if err != nil {
		return keyTaken, err
	}
	attrs, err := handle.Attrs(ctx)
	if errors.Is(err, gcsstorage.ErrObjectNotExist) {
		return keyVacant, nil
	}
	if err != nil {
		return keyTaken, mapError("put", err)
	}
	if attrs.Metadata[tokenMetadata] == token && attrs.Metadata[shaMetadata] == obj.SHA256 && attrs.Size == obj.Size {
		return committedHere, nil
	}
	return keyTaken, nil
}

// isPrecondition reports whether err is the 412 a create-only write returns
// when its key exists.
func isPrecondition(err error) bool {
	var g *googleapi.Error
	return errors.As(err, &g) && g.Code == 412
}

// randomToken returns a fresh write token: 16 bytes from crypto/rand, hex.
func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("storage/gcs: write token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// releaseSpoolFile is the default release: best effort, see storagekit.SpoolFile.
func releaseSpoolFile(sp *storagekit.SpoolFile) error { return sp.Release() }

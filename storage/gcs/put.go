package gcs

import (
	"context"
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
	if _, err := s.sdk(); err != nil {
		return storage.Object{}, err
	}
	inspected, err := storage.Inspect(r, opts)
	if err != nil {
		return storage.Object{}, err
	}
	spool, err := storagekit.Spool(ctx, "storage/gcs", s.tempDir, "tango-gcs-*", inspected)
	if err != nil {
		return storage.Object{}, err
	}
	defer func() {
		// A close failure surfaces only when the upload has not already failed.
		if closeErr := storagekit.ReleaseSpool("storage/gcs", spool); err == nil && closeErr != nil {
			obj, err = storage.Object{}, closeErr
		}
	}()
	obj = inspected.Object("")

	key := s.nextKey()
	for attempt := 0; attempt < storagekit.KeyAttempts; attempt++ {
		if !storage.ValidKey(key) {
			return storage.Object{}, fmt.Errorf("storage/gcs: generated key %q is invalid", key)
		}
		err := s.create(ctx, spool, obj, key)
		if err == nil {
			obj.Key = key
			return obj, nil
		}
		if !isPrecondition(err) {
			return storage.Object{}, mapError("put", err)
		}
		state, err := s.conflict(ctx, key, obj.SHA256)
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
func (s *Store) create(ctx context.Context, spool *os.File, obj storage.Object, key string) error {
	handle, err := s.object(key)
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
	w.Metadata = map[string]string{shaMetadata: obj.SHA256}
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

// conflict reads the object at key once and says what the 412 meant.
func (s *Store) conflict(ctx context.Context, key, sha string) (conflictState, error) {
	handle, err := s.object(key)
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
	if attrs.Metadata[shaMetadata] == sha {
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

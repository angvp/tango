// Package local is a storage.Store on the local filesystem, for development,
// tests and single-host deployments with a persistent volume. It is not
// shared between machines; use an object store such as storage/s3 for more
// than one instance or an ephemeral disk.
//
// Everything lives under one root directory, reached only through os.Root
// so no key or symlink can leave it. An object is a directory holding its
// bytes (data) and its description (meta), published by renaming a complete
// temporary directory into place: the bytes and the metadata appear
// together or not at all, and a crash leaves only a temporary directory
// that no read ever sees and the next New sweeps.
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"syscall"
	"time"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	// objectsDir holds published objects, fanned out by the first two
	// characters of the key. tmpDir holds writes in progress and deleted
	// objects on their way out.
	objectsDir = "objects"
	tmpDir     = "tmp"

	// trashPrefix names a deleted object on its way out of tmp.
	trashPrefix = "deleted-"

	dataName = "data"
	metaName = "meta"

	// staleAfter is how old a temporary directory must be before New
	// sweeps it, so opening a second Store on the same root never removes
	// another writer's work in progress.
	staleAfter = time.Hour
)

// Store is a filesystem storage.Store. Close it when done.
type Store struct {
	root    *os.Root
	nextKey func() string
}

// meta is the stored description of an object.
type meta struct {
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	ContentType string    `json:"content_type"`
	Created     time.Time `json:"created"`
}

// New opens, creating it if needed, the Store rooted at dir. Directories are
// created 0700 and files 0600. It removes temporary directories left by an
// interrupted write.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("storage/local: root directory is empty")
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("storage/local: create root: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("storage/local: open root: %w", err)
	}
	s := &Store{root: root, nextKey: storage.NewKey}
	for _, sub := range []string{objectsDir, tmpDir} {
		if err := root.MkdirAll(sub, dirMode); err != nil {
			root.Close()
			return nil, fmt.Errorf("storage/local: create %s: %w", sub, err)
		}
	}
	if err := s.sweep(); err != nil {
		root.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the root directory.
func (s *Store) Close() error { return s.root.Close() }

// sweep removes temporary directories older than staleAfter.
func (s *Store) sweep() error {
	entries, err := fs.ReadDir(s.root.FS(), tmpDir)
	if err != nil {
		return fmt.Errorf("storage/local: read %s: %w", tmpDir, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < staleAfter {
			continue
		}
		if err := s.root.RemoveAll(path.Join(tmpDir, entry.Name())); err != nil {
			return fmt.Errorf("storage/local: sweep %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func objectPath(key string) string { return path.Join(objectsDir, key[:2], key) }

// Put implements storage.Store.
func (s *Store) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := ctx.Err(); err != nil {
		return storage.Object{}, err
	}
	inspected, err := storage.Inspect(r, opts)
	if err != nil {
		return storage.Object{}, err
	}
	staging := path.Join(tmpDir, storage.NewKey())
	if err := s.root.Mkdir(staging, dirMode); err != nil {
		return storage.Object{}, fmt.Errorf("storage/local: stage: %w", err)
	}
	// Until publish succeeds the staging directory is garbage; after it, the
	// rename has moved it away and there is nothing left to remove.
	defer func() { _ = s.root.RemoveAll(staging) }()

	if err := s.stage(ctx, staging, inspected); err != nil {
		return storage.Object{}, err
	}
	key, err := s.publish(staging)
	if err != nil {
		return storage.Object{}, err
	}
	return inspected.Object(key), nil
}

// stage writes the object's bytes and description into the staging
// directory and syncs them.
func (s *Store) stage(ctx context.Context, staging string, inspected *storage.Inspector) error {
	if err := s.writeFile(path.Join(staging, dataName), &storagekit.ContextReader{Ctx: ctx, R: inspected}); err != nil {
		return err
	}
	obj := inspected.Object("")
	description, err := json.Marshal(meta{Size: obj.Size, SHA256: obj.SHA256, ContentType: obj.ContentType, Created: time.Now().UTC()})
	if err != nil {
		return err
	}
	if err := s.writeFile(path.Join(staging, metaName), bytes.NewReader(description)); err != nil {
		return err
	}
	return s.syncDir(staging)
}

// publish renames the staging directory to a new object's path and returns
// its key. A key that is taken is retried with a new one; if flushing the
// published entry fails, the object is removed again so a failed Put leaves
// nothing behind.
func (s *Store) publish(staging string) (string, error) {
	for attempt := 0; attempt < storagekit.KeyAttempts; attempt++ {
		key := s.nextKey()
		if !storage.ValidKey(key) {
			return "", fmt.Errorf("storage/local: generated key %q is invalid", key)
		}
		parent := path.Dir(objectPath(key))
		if err := s.root.MkdirAll(parent, dirMode); err != nil {
			return "", fmt.Errorf("storage/local: create %s: %w", parent, err)
		}
		err := s.root.Rename(staging, objectPath(key))
		if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTEMPTY) {
			continue // a collision: the published object stays untouched
		}
		if err != nil {
			return "", fmt.Errorf("storage/local: publish: %w", err)
		}
		if err := s.syncDir(parent); err != nil {
			return "", errors.Join(err, s.removeObject(key))
		}
		return key, nil
	}
	return "", storage.ErrExists
}

// writeFile creates name exclusively, copies r into it and syncs it.
func (s *Store) writeFile(name string, r io.Reader) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("storage/local: create %s: %w", path.Base(name), err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("storage/local: sync %s: %w", path.Base(name), err)
	}
	return f.Close()
}

// syncDir flushes a directory's entries so a published object survives a
// crash. A filesystem that cannot sync a directory is not an error.
func (s *Store) syncDir(name string) error {
	d, err := s.root.Open(name)
	if err != nil {
		return fmt.Errorf("storage/local: open %s: %w", name, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, errors.ErrUnsupported) {
		return fmt.Errorf("storage/local: sync %s: %w", name, err)
	}
	return nil
}

// Stat implements storage.Store. An object is present only when its
// directory holds a readable meta and a data file of the size meta records.
func (s *Store) Stat(ctx context.Context, key string) (storage.Info, error) {
	if err := storagekit.Check(ctx, key); err != nil {
		return storage.Info{}, err
	}
	return s.info(key)
}

func (s *Store) info(key string) (storage.Info, error) {
	dir := objectPath(key)
	raw, err := s.root.ReadFile(path.Join(dir, metaName))
	if err != nil {
		return storage.Info{}, absent(err)
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil || m.Size < 0 || len(m.SHA256) != 64 {
		return storage.Info{}, storage.ErrNotFound
	}
	data, err := s.root.Stat(path.Join(dir, dataName))
	if err != nil {
		return storage.Info{}, absent(err)
	}
	if !data.Mode().IsRegular() || data.Size() != m.Size {
		return storage.Info{}, storage.ErrNotFound
	}
	return storage.Info{
		Key: key, Size: m.Size, SHA256: m.SHA256, ContentType: m.ContentType,
		ETag: storage.ETagFor(m.SHA256), ModTime: m.Created,
	}, nil
}

// absent turns "the path is not there" into storage.ErrNotFound and leaves
// every other failure, including a path that escapes the root, as an error.
func absent(err error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return storage.ErrNotFound
	}
	return fmt.Errorf("storage/local: %w", err)
}

// Open implements storage.Store.
func (s *Store) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if err := storagekit.Check(ctx, key); err != nil {
		return nil, err
	}
	info, err := s.info(key)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset > info.Size {
		return nil, storage.ErrInvalidRange
	}
	end := info.Size
	if length >= 0 && offset+length < info.Size {
		end = offset + length
	}
	f, err := s.root.Open(path.Join(objectPath(key), dataName))
	if err != nil {
		return nil, absent(err)
	}
	return &section{SectionReader: io.NewSectionReader(f, offset, end-offset), file: f}, nil
}

// Delete implements storage.Store. The object leaves by one rename, so it is
// never half there, and its bytes are removed afterwards.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := storagekit.Check(ctx, key); err != nil {
		return err
	}
	return s.removeObject(key)
}

// removeObject renames the object's directory out of place, then removes it.
// A missing object is not an error.
func (s *Store) removeObject(key string) error {
	trash := path.Join(tmpDir, trashPrefix+storage.NewKey())
	if err := s.root.Rename(objectPath(key), trash); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("storage/local: delete: %w", err)
	}
	if err := s.root.RemoveAll(trash); err != nil {
		return fmt.Errorf("storage/local: delete: %w", err)
	}
	return nil
}

// section reads a range of a file and closes the file with it.
type section struct {
	*io.SectionReader
	file *os.File
}

func (s *section) Close() error { return s.file.Close() }

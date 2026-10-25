package storagekit

import (
	"context"
	"fmt"
	"io"
	"os"
)

// FileOps are the file operations a SpoolFile performs. The zero value uses the
// operating system; a test substitutes failing operations.
type FileOps struct {
	Sync   func(*os.File) error
	Close  func(*os.File) error
	Remove func(string) error
}

func (o FileOps) sync(f *os.File) error {
	if o.Sync != nil {
		return o.Sync(f)
	}
	return f.Sync()
}

func (o FileOps) close(f *os.File) error {
	if o.Close != nil {
		return o.Close(f)
	}
	return f.Close()
}

func (o FileOps) remove(name string) error {
	if o.Remove != nil {
		return o.Remove(name)
	}
	return os.Remove(name)
}

// SpoolFile is a temporary file holding one upload body. Its life has three
// steps. Fill writes the body, syncs it and closes it: every failure of those
// is returned before any upload begins, so an upload never starts on bytes
// that did not reach the file. Open reopens the completed file read-only for
// the upload. Release closes what is still open and removes the file.
//
// Release is best effort: it always tries to close and remove, but a failure
// it reports cannot undo an upload that has already committed, so a caller
// that has committed must not treat a Release error as a failed write.
type SpoolFile struct {
	prefix string
	path   string
	ops    FileOps
	w      *os.File // writable until Fill closes it
	r      *os.File // read-only, from Open
}

// NewSpoolFile creates the file in dir. prefix names the adapter in errors.
func NewSpoolFile(prefix, dir, pattern string, ops FileOps) (*SpoolFile, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: spool: %w", prefix, err)
	}
	return &SpoolFile{prefix: prefix, path: f.Name(), ops: ops, w: f}, nil
}

// Fill copies r into the file, then syncs and closes it. It reports the first
// failure of the copy, the sync or the close.
func (s *SpoolFile) Fill(ctx context.Context, r io.Reader) error {
	_, copyErr := io.Copy(s.w, &ContextReader{Ctx: ctx, R: r})
	syncErr := s.ops.sync(s.w)
	closeErr := s.ops.close(s.w)
	s.w = nil
	switch {
	case copyErr != nil:
		return fmt.Errorf("%s: spool: %w", s.prefix, copyErr)
	case syncErr != nil:
		return fmt.Errorf("%s: spool sync: %w", s.prefix, syncErr)
	case closeErr != nil:
		return fmt.Errorf("%s: spool close: %w", s.prefix, closeErr)
	}
	return nil
}

// Open reopens the completed file read-only. Release closes the handle.
func (s *SpoolFile) Open() (*os.File, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("%s: spool open: %w", s.prefix, err)
	}
	s.r = f
	return f, nil
}

// Release closes any handle still open, then removes the file. It attempts both
// steps even when one fails, and returns the first failure.
func (s *SpoolFile) Release() error {
	var first error
	if s.w != nil {
		if err := s.ops.close(s.w); err != nil {
			first = err
		}
		s.w = nil
	}
	if s.r != nil {
		if err := s.ops.close(s.r); err != nil && first == nil {
			first = err
		}
		s.r = nil
	}
	if err := s.ops.remove(s.path); err != nil && first == nil {
		first = err
	}
	if first != nil {
		return fmt.Errorf("%s: spool release: %w", s.prefix, first)
	}
	return nil
}

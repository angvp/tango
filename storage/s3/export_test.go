package s3

import "github.com/angvp/tango/internal/storagekit"

// SetKeyFunc makes the Store take its generated keys from next, so a test can
// force a collision.
func (s *Store) SetKeyFunc(next func() string) { s.nextKey = next }

// SetSpoolOps replaces the spool's file operations, so a test can fail one.
func (s *Store) SetSpoolOps(ops storagekit.FileOps) { s.spoolOps = ops }

// SetReleaseSpool replaces how Put releases its spool. A replacement should
// call the real release to keep the file from leaking.
func (s *Store) SetReleaseSpool(next func(*storagekit.SpoolFile) error) { s.releaseSpool = next }

package storagekit_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/angvp/tango/internal/storagekit"
)

// onlySpoolEntries returns the names of the files left in dir.
func onlySpoolEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) = %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// filledSpool returns a spool in dir whose Fill has succeeded.
func filledSpool(t *testing.T, dir string, ops storagekit.FileOps) *storagekit.SpoolFile {
	t.Helper()
	sp, err := storagekit.NewSpoolFile("pfx", dir, "spool-*", ops)
	if err != nil {
		t.Fatalf("NewSpoolFile = %v", err)
	}
	if err := sp.Fill(context.Background(), strings.NewReader("payload")); err != nil {
		t.Fatalf("Fill = %v", err)
	}
	return sp
}

func TestNewSpoolFileReportsACreateFailure(t *testing.T) {
	tests := []struct {
		name string
		dir  func(t *testing.T) string
		want error
	}{
		{
			name: "directory does not exist",
			dir:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") },
			want: fs.ErrNotExist,
		},
		{
			name: "path is a regular file, not a directory",
			dir: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "plain")
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			},
			want: syscall.ENOTDIR,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp, err := storagekit.NewSpoolFile("storage/x", tt.dir(t), "spool-*", storagekit.FileOps{})
			if sp != nil {
				t.Fatalf("NewSpoolFile returned a spool alongside error %v", err)
			}
			if !errors.Is(err, tt.want) || !strings.HasPrefix(err.Error(), "storage/x: spool: ") {
				t.Fatalf("NewSpoolFile = %v; want storage/x: spool: wrapping %v", err, tt.want)
			}
		})
	}
}

func TestNewSpoolFileRefusesAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission bits")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	// Registered after TempDir's own cleanup, so it runs first and lets the
	// directory be removed.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	sp, err := storagekit.NewSpoolFile("pfx", dir, "spool-*", storagekit.FileOps{})
	if sp != nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("NewSpoolFile in a read-only dir = %v, %v; want fs.ErrPermission", sp, err)
	}
}

func TestReleaseWithoutFillClosesTheWritableHandleAndReportsItsFailure(t *testing.T) {
	dir := t.TempDir()
	sp, err := storagekit.NewSpoolFile("storage/x", dir, "spool-*", closeFails())
	if err != nil {
		t.Fatal(err)
	}
	err = sp.Release()
	if !errors.Is(err, errInjected) || !strings.HasPrefix(err.Error(), "storage/x: spool release: ") {
		t.Fatalf("Release before Fill = %v; want storage/x: spool release: wrapping the close failure", err)
	}
	if left := onlySpoolEntries(t, dir); len(left) != 0 {
		t.Fatalf("%d file(s) left after Release", len(left))
	}
}

func TestReleaseStillClosesAndReportsARemoveFailure(t *testing.T) {
	dir := t.TempDir()
	removeFails := storagekit.FileOps{Remove: func(name string) error {
		// Remove the file for real so the test leaks nothing, then fail.
		_ = os.Remove(name)
		return errInjected
	}}
	sp := filledSpool(t, dir, removeFails)
	err := sp.Release()
	if !errors.Is(err, errInjected) || !strings.HasPrefix(err.Error(), "pfx: spool release: ") {
		t.Fatalf("Release = %v; want pfx: spool release: wrapping the remove failure", err)
	}
}

func TestOpenAndReleaseFailWhenTheFileWasRemovedOutFromUnder(t *testing.T) {
	dir := t.TempDir()
	sp := filledSpool(t, dir, storagekit.FileOps{})
	left := onlySpoolEntries(t, dir)
	if len(left) != 1 {
		t.Fatalf("spool has %d files after Fill; want 1", len(left))
	}
	if err := os.Remove(filepath.Join(dir, left[0])); err != nil {
		t.Fatal(err)
	}

	f, err := sp.Open()
	if f != nil || !errors.Is(err, fs.ErrNotExist) || !strings.HasPrefix(err.Error(), "pfx: spool open: ") {
		t.Fatalf("Open = %v, %v; want pfx: spool open: wrapping fs.ErrNotExist", f, err)
	}
	if err := sp.Release(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Release = %v; want fs.ErrNotExist from the remove", err)
	}
}

func TestOpenAfterReleaseFailsAndASecondReleaseReportsTheMissingFile(t *testing.T) {
	sp := filledSpool(t, t.TempDir(), storagekit.FileOps{})
	if err := sp.Release(); err != nil {
		t.Fatalf("first Release = %v", err)
	}
	if f, err := sp.Open(); f != nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open after Release = %v, %v; want fs.ErrNotExist", f, err)
	}
	if err := sp.Release(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("second Release = %v; want fs.ErrNotExist", err)
	}
}

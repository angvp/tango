package local_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"
	"github.com/angvp/tango/storagetest"
)

func open(t *testing.T) (*local.Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "uploads")
	s, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, root
}

func TestLocalPassesTheConformanceSuite(t *testing.T) {
	storagetest.Run(t, storagetest.Factory{
		New: func(t *testing.T) storage.Store { s, _ := open(t); return s },
		NewWithKeys: func(t *testing.T, next func() string) storage.Store {
			s, err := local.NewWithKeys(filepath.Join(t.TempDir(), "uploads"), next)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			return s
		},
	})
}

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00"), bytes.Repeat([]byte("x"), 300)...)

// publishedDir is where an object with key lives under root.
func publishedDir(root, key string) string { return filepath.Join(root, "objects", key[:2], key) }

func TestAPartialObjectIsInvisibleToStatAndOpen(t *testing.T) {
	s, root := open(t)
	good, err := s.Put(context.Background(), bytes.NewReader(pngBytes), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	dir := publishedDir(root, good.Key)
	data, _ := os.ReadFile(filepath.Join(dir, "data"))
	meta, _ := os.ReadFile(filepath.Join(dir, "meta"))

	plant := func(files map[string][]byte) string {
		key := storage.NewKey()
		d := publishedDir(root, key)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(d, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return key
	}
	for name, files := range map[string]map[string][]byte{
		"an empty directory":        {},
		"data without meta":         {"data": data},
		"meta without data":         {"meta": meta},
		"corrupt meta":              {"data": data, "meta": []byte("{not json")},
		"data shorter than meta":    {"data": data[:10], "meta": meta},
		"a file where a dir should": nil,
	} {
		t.Run(name, func(t *testing.T) {
			var key string
			if files == nil {
				key = storage.NewKey()
				if err := os.MkdirAll(filepath.Dir(publishedDir(root, key)), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(publishedDir(root, key), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				key = plant(files)
			}
			if _, err := s.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Stat: %v, want ErrNotFound", err)
			}
			if _, err := s.Open(context.Background(), key, 0, -1); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Open: %v, want ErrNotFound", err)
			}
			if err := s.Delete(context.Background(), key); err != nil {
				t.Errorf("Delete: %v, want nil", err)
			}
		})
	}
	if _, err := s.Stat(context.Background(), good.Key); err != nil {
		t.Fatalf("the complete object disappeared: %v", err)
	}
}

func TestAnInterruptedWriteLeavesNothingVisibleAndIsSweptOnOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	s, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	// What a crash between "write the bytes" and "publish" leaves behind.
	stale := filepath.Join(root, "tmp", "crashed")
	fresh := filepath.Join(root, "tmp", "in-flight")
	for _, d := range []string{stale, fresh} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "data"), pngBytes, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	s, err = local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stale temporary directory survived opening: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a recent temporary directory (another writer's) was swept: %v", err)
	}
}

func TestRefusedAndFailedWritesLeaveNoTemporaryFiles(t *testing.T) {
	s, root := open(t)
	_, _ = s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 5000)), storage.PutOptions{MaxSize: 100})
	_, _ = s.Put(context.Background(), strings.NewReader("<html>"), storage.PutOptions{MaxSize: 100, AllowedTypes: []string{"image/*"}})
	entries, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("tmp has %d entries (err %v), want none", len(entries), err)
	}
}

func TestPermissionsAreOwnerOnly(t *testing.T) {
	s, root := open(t)
	obj, err := s.Put(context.Background(), bytes.NewReader(pngBytes), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		root:                           0o700,
		filepath.Join(root, "objects"): 0o700,
		publishedDir(root, obj.Key):    0o700,
		filepath.Join(publishedDir(root, obj.Key), "data"): 0o600,
		filepath.Join(publishedDir(root, obj.Key), "meta"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: mode %v (err %v), want %v", path, info.Mode().Perm(), err, want)
		}
	}
}

func TestASymlinkOutOfTheRootIsNeverFollowed(t *testing.T) {
	s, root := open(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "data"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "meta"), []byte(`{"size":6,"sha256":"00","content_type":"text/plain","created":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key := storage.NewKey()
	if err := os.MkdirAll(filepath.Dir(publishedDir(root, key)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, publishedDir(root, key)); err != nil {
		t.Fatal(err)
	}
	if rc, err := s.Open(context.Background(), key, 0, -1); err == nil {
		rc.Close()
		t.Fatal("Open followed a symlink out of the root")
	}
	if _, err := s.Stat(context.Background(), key); err == nil {
		t.Fatal("Stat followed a symlink out of the root")
	}
	if err := s.Delete(context.Background(), key); err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Logf("Delete refused the symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "data")); err != nil {
		t.Fatalf("Delete removed a file outside the root: %v", err)
	}
}

func TestNewRefusesAnEmptyRoot(t *testing.T) {
	if _, err := local.New(""); err == nil {
		t.Fatal("New(\"\") succeeded")
	}
}

func TestNewRefusesExistingDirectoriesOpenToGroupOrOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows, where local.New skips the check")
	}
	for _, tt := range []struct {
		name string
		make func(t *testing.T, root string)
	}{
		{"the root", func(t *testing.T, root string) { mkdir(t, root, 0o755) }},
		{"the root, group-writable", func(t *testing.T, root string) { mkdir(t, root, 0o770) }},
		{"objects", func(t *testing.T, root string) {
			mkdir(t, root, 0o700)
			mkdir(t, filepath.Join(root, "objects"), 0o750)
		}},
		{"tmp", func(t *testing.T, root string) {
			mkdir(t, root, 0o700)
			mkdir(t, filepath.Join(root, "objects"), 0o700)
			mkdir(t, filepath.Join(root, "tmp"), 0o705)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "uploads")
			tt.make(t, root)
			s, err := local.New(root)
			if err == nil {
				s.Close()
				t.Fatal("New accepted a directory that grants group or other access")
			}
			if !strings.Contains(err.Error(), "chmod 700") {
				t.Errorf("error %q does not tell the operator how to fix it", err)
			}
		})
	}
}

func TestNewLeavesAPermissiveDirectoryAsItFoundIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows, where local.New skips the check")
	}
	root := filepath.Join(t.TempDir(), "uploads")
	mkdir(t, root, 0o755)
	if s, err := local.New(root); err == nil {
		s.Close()
		t.Fatal("New accepted a 0755 root")
	}
	if info, err := os.Stat(root); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("the operator's directory was changed: %v, %v", info.Mode().Perm(), err)
	}
}

func TestNewAcceptsPrivateExistingDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	for _, dir := range []string{root, filepath.Join(root, "objects"), filepath.Join(root, "tmp")} {
		mkdir(t, dir, 0o700)
	}
	s, err := local.New(root)
	if err != nil {
		t.Fatalf("New refused private directories: %v", err)
	}
	defer s.Close()
	if _, err := s.Put(context.Background(), bytes.NewReader(pngBytes), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatal(err)
	}
}

// mkdir creates dir with exactly mode, whatever the umask.
func mkdir(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
}

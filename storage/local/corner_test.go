package local_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"
)

func TestNewRefusesARootThatIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := local.New(file); err == nil {
		s.Close()
		t.Fatal("New accepted a regular file as its root")
	}
	if s, err := local.New(filepath.Join(file, "below")); err == nil {
		s.Close()
		t.Fatal("New accepted a root below a regular file")
	}
}

func TestACanceledOrBrokenSourceLeavesNoObjectBehind(t *testing.T) {
	s, root := open(t)
	boom := errors.New("socket reset")
	source := io.MultiReader(bytes.NewReader(pngBytes), iotest.ErrReader(boom))
	if _, err := s.Put(context.Background(), source, storage.PutOptions{MaxSize: 1 << 20}); !errors.Is(err, boom) {
		t.Fatalf("Put = %v, want the source error", err)
	}
	for _, dir := range []string{"objects", "tmp"} {
		var leftovers []string
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				leftovers = append(leftovers, path)
			}
			return nil
		})
		if len(leftovers) != 0 {
			t.Fatalf("files left under %s: %v", dir, leftovers)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(root, "tmp"))
	if len(entries) != 0 {
		t.Fatalf("staging directories left behind: %d", len(entries))
	}
}

func TestACloseStoreFailsEveryCallInsteadOfPanicking(t *testing.T) {
	s, _ := open(t)
	obj, err := s.Put(context.Background(), bytes.NewReader(pngBytes), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Put(ctx, bytes.NewReader(pngBytes), storage.PutOptions{MaxSize: 1 << 20}); err == nil {
		t.Error("Put on a closed store succeeded")
	}
	if _, err := s.Stat(ctx, obj.Key); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat on a closed store = %v, want a real error, not a miss", err)
	}
	if _, err := s.Open(ctx, obj.Key, 0, -1); err == nil {
		t.Error("Open on a closed store succeeded")
	}
	if err := s.Delete(ctx, obj.Key); err == nil {
		t.Error("Delete on a closed store reported success for an object it could not remove")
	}
}

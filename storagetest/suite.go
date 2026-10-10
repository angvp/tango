// Package storagetest holds the conformance suite every storage.Store must
// pass, and NewMemory, an in-memory Store for tests that need no disk and no
// credentials.
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/angvp/tango/storage"
)

// Factory builds the Store under test. New is required. NewWithKeys is
// optional: it builds a Store whose generated keys come from next, which lets
// the suite prove that a refused write stores nothing and that a key
// collision never overwrites; those checks are skipped without it.
type Factory struct {
	New         func(t *testing.T) storage.Store
	NewWithKeys func(t *testing.T, next func() string) storage.Store
}

var png = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00"), bytes.Repeat([]byte("0123456789"), 100)...)

func opts() storage.PutOptions { return storage.PutOptions{MaxSize: 1 << 20} }

// Run runs the conformance suite against the Store f builds.
func Run(t *testing.T, f Factory) {
	t.Helper()
	t.Run("PutThenStatAndOpen", func(t *testing.T) { putStatOpen(t, f.New(t)) })
	t.Run("OpenRanges", func(t *testing.T) { openRanges(t, f.New(t)) })
	t.Run("MissingObjects", func(t *testing.T) { missing(t, f.New(t)) })
	t.Run("InvalidKeys", func(t *testing.T) { invalidKeys(t, f.New(t)) })
	t.Run("InvalidOptions", func(t *testing.T) { invalidOptions(t, f.New(t)) })
	t.Run("SizeCapIsExact", func(t *testing.T) { sizeCap(t, f.New(t)) })
	t.Run("ContextIsHonoured", func(t *testing.T) { cancelled(t, f.New(t)) })
	t.Run("ConcurrentPuts", func(t *testing.T) { concurrent(t, f.New(t)) })
	t.Run("DeleteIsIdempotent", func(t *testing.T) { deleteTwice(t, f.New(t)) })
	if f.NewWithKeys == nil {
		return
	}
	t.Run("RefusedWritesStoreNothing", func(t *testing.T) { refusedStoreNothing(t, f.NewWithKeys) })
	t.Run("CreateOnlyNeverOverwrites", func(t *testing.T) { createOnly(t, f.NewWithKeys) })
}

func mustPut(t *testing.T, s storage.Store, data []byte) storage.Object {
	t.Helper()
	obj, err := s.Put(context.Background(), bytes.NewReader(data), opts())
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return obj
}

func readAll(t *testing.T, s storage.Store, key string, offset, length int64) ([]byte, error) {
	t.Helper()
	rc, err := s.Open(context.Background(), key, offset, length)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func putStatOpen(t *testing.T, s storage.Store) {
	obj := mustPut(t, s, png)
	if !storage.ValidKey(obj.Key) || obj.Size != int64(len(png)) || obj.ContentType != "image/png" || len(obj.SHA256) != 64 {
		t.Fatalf("Put returned %+v", obj)
	}
	info, err := s.Stat(context.Background(), obj.Key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Key != obj.Key || info.Size != obj.Size || info.SHA256 != obj.SHA256 || info.ContentType != obj.ContentType ||
		info.ETag != storage.ETagFor(obj.SHA256) || info.ModTime.IsZero() {
		t.Fatalf("Stat = %+v, Put = %+v", info, obj)
	}
	if got, err := readAll(t, s, obj.Key, 0, -1); err != nil || !bytes.Equal(got, png) {
		t.Fatalf("Open read %d bytes, err %v; want the stored %d", len(got), err, len(png))
	}
	empty := mustPut(t, s, nil)
	if got, err := readAll(t, s, empty.Key, 0, -1); err != nil || len(got) != 0 || empty.Size != 0 {
		t.Fatalf("empty object: %d bytes, err %v, %+v", len(got), err, empty)
	}
}

func openRanges(t *testing.T, s storage.Store) {
	data := []byte("0123456789")
	key := mustPut(t, s, data).Key
	for _, tt := range []struct {
		name           string
		offset, length int64
		want           string
	}{
		{"all", 0, -1, "0123456789"},
		{"a prefix", 0, 4, "0123"},
		{"the middle", 3, 4, "3456"},
		{"to the end", 7, -1, "789"},
		{"a length past the end is shortened", 8, 100, "89"},
		{"zero length", 5, 0, ""},
		{"offset at the end", 10, -1, ""},
		{"a maximal length from the start", 0, math.MaxInt64, "0123456789"},
		{"a maximal length from the middle", 3, math.MaxInt64, "3456789"},
		{"a maximal length at the end", 10, math.MaxInt64, ""},
		{"a length that exactly reaches the end", 4, 6, "456789"},
		{"a length one past the end", 4, 7, "456789"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readAll(t, s, key, tt.offset, tt.length)
			if err != nil || string(got) != tt.want {
				t.Fatalf("Open(%d, %d) = %q, %v; want %q", tt.offset, tt.length, got, err, tt.want)
			}
		})
	}
	for _, offset := range []int64{-1, 11, 1 << 40} {
		if _, err := s.Open(context.Background(), key, offset, 1); !errors.Is(err, storage.ErrInvalidRange) {
			t.Errorf("Open offset %d: err = %v, want ErrInvalidRange", offset, err)
		}
	}
}

func missing(t *testing.T, s storage.Store) {
	key := storage.NewKey()
	if _, err := s.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat: %v, want ErrNotFound", err)
	}
	if _, err := s.Open(context.Background(), key, 0, -1); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Open: %v, want ErrNotFound", err)
	}
	if err := s.Delete(context.Background(), key); err != nil {
		t.Errorf("Delete of a missing object: %v, want nil", err)
	}
}

func invalidKeys(t *testing.T, s storage.Store) {
	good := storage.NewKey()
	for _, key := range []string{"", "x", "../../etc/passwd", good + "a", strings.ToUpper(good), good[:16] + "/" + good[17:], good[:31] + ".", good[:31] + "\x00"} {
		ctx := context.Background()
		if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Stat(%q): %v, want ErrInvalidKey", key, err)
		}
		if _, err := s.Open(ctx, key, 0, -1); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Open(%q): %v, want ErrInvalidKey", key, err)
		}
		if err := s.Delete(ctx, key); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Delete(%q): %v, want ErrInvalidKey", key, err)
		}
	}
}

func invalidOptions(t *testing.T, s storage.Store) {
	for _, max := range []int64{0, -5} {
		if _, err := s.Put(context.Background(), strings.NewReader("x"), storage.PutOptions{MaxSize: max}); !errors.Is(err, storage.ErrInvalidOptions) {
			t.Errorf("MaxSize %d: %v, want ErrInvalidOptions", max, err)
		}
	}
}

func sizeCap(t *testing.T, s storage.Store) {
	at := storage.PutOptions{MaxSize: 100}
	if obj, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 100)), at); err != nil || obj.Size != 100 {
		t.Fatalf("exactly the cap: %+v, %v", obj, err)
	}
	var tooLarge *storage.TooLargeError
	_, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 101)), at)
	if !errors.Is(err, storage.ErrTooLarge) || !errors.As(err, &tooLarge) || tooLarge.Max != 100 {
		t.Fatalf("one over: %v, want ErrTooLarge with Max 100", err)
	}
}

func cancelled(t *testing.T, s storage.Store) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Put(ctx, bytes.NewReader(png), opts()); !errors.Is(err, context.Canceled) {
		t.Errorf("Put with a cancelled context: %v, want context.Canceled", err)
	}
	key := mustPut(t, s, png).Key
	if _, err := s.Stat(ctx, key); !errors.Is(err, context.Canceled) {
		t.Errorf("Stat with a cancelled context: %v, want context.Canceled", err)
	}
}

func concurrent(t *testing.T, s storage.Store) {
	const n = 16
	keys := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			obj, err := s.Put(context.Background(), bytes.NewReader(append([]byte{byte(i)}, png...)), opts())
			if err != nil {
				t.Error(err)
				return
			}
			keys[i] = obj.Key
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, key := range keys {
		if seen[key] {
			t.Fatalf("key %q issued twice", key)
		}
		seen[key] = true
		if got, err := readAll(t, s, key, 0, 1); err != nil || len(got) != 1 || got[0] != byte(i) {
			t.Fatalf("object %d: first byte %v, err %v", i, got, err)
		}
	}
}

func deleteTwice(t *testing.T, s storage.Store) {
	key := mustPut(t, s, png).Key
	for i := 0; i < 2; i++ {
		if err := s.Delete(context.Background(), key); err != nil {
			t.Fatalf("Delete #%d: %v", i+1, err)
		}
	}
	if _, err := s.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat after Delete: %v, want ErrNotFound", err)
	}
}

func refusedStoreNothing(t *testing.T, build func(*testing.T, func() string) storage.Store) {
	key := storage.NewKey()
	s := build(t, func() string { return key })
	notStored := func(name string) {
		t.Helper()
		if _, err := s.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("after %s, Stat = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 2000)), storage.PutOptions{MaxSize: 1000}); !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	notStored("a too-large write")
	if _, err := s.Put(context.Background(), strings.NewReader("<html>x</html>"), storage.PutOptions{MaxSize: 1000, AllowedTypes: []string{"image/*"}}); !errors.Is(err, storage.ErrTypeNotAllowed) {
		t.Fatalf("disallowed type: %v", err)
	}
	notStored("a disallowed type")
	boom := errors.New("connection reset")
	if _, err := s.Put(context.Background(), io.MultiReader(bytes.NewReader(png), iotest.ErrReader(boom)), opts()); !errors.Is(err, boom) {
		t.Fatalf("reader error: %v", err)
	}
	notStored("a failing reader")
}

func createOnly(t *testing.T, build func(*testing.T, func() string) storage.Store) {
	key := storage.NewKey()
	s := build(t, func() string { return key })
	first := mustPut(t, s, png)
	if first.Key != key {
		t.Fatalf("Put used key %q, want %q", first.Key, key)
	}
	if _, err := s.Put(context.Background(), strings.NewReader("different"), opts()); !errors.Is(err, storage.ErrExists) {
		t.Fatalf("colliding Put: %v, want ErrExists", err)
	}
	if got, err := readAll(t, s, key, 0, -1); err != nil || !bytes.Equal(got, png) {
		t.Fatalf("the first object changed: %d bytes, err %v", len(got), err)
	}
}

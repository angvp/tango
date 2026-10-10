package storage_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/angvp/tango/storage"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

func TestInspectPassesBytesThroughAndReportsWhatItSaw(t *testing.T) {
	data := append(append([]byte{}, pngHeader...), bytes.Repeat([]byte{7}, 5000)...)
	in, err := storage.Inspect(bytes.NewReader(data), storage.PutOptions{MaxSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(in)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes, err %v; want the %d bytes unchanged", len(got), err, len(data))
	}
	sum := sha256.Sum256(data)
	obj := in.Object("k")
	if obj.Key != "k" || obj.Size != int64(len(data)) || obj.ContentType != "image/png" || obj.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("Object = %+v", obj)
	}
}

func TestInspectSniffsTheStreamNotAnyDeclaredType(t *testing.T) {
	in, err := storage.Inspect(strings.NewReader("<html><script>alert(1)</script></html>"), storage.PutOptions{MaxSize: 1024, AllowedTypes: []string{"text/html"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, in); err != nil {
		t.Fatal(err)
	}
	if got := in.Object("k").ContentType; !strings.HasPrefix(got, "text/html") {
		t.Fatalf("ContentType = %q, want text/html…", got)
	}
}

func TestInspectEnforcesTheSizeCapExactly(t *testing.T) {
	for _, tt := range []struct {
		name string
		size int
		ok   bool
	}{{"exactly the cap", 100, true}, {"one over", 101, false}, {"far over", 100000, false}} {
		t.Run(tt.name, func(t *testing.T) {
			in, err := storage.Inspect(bytes.NewReader(bytes.Repeat([]byte("a"), tt.size)), storage.PutOptions{MaxSize: 100})
			if err == nil {
				_, err = io.Copy(io.Discard, in)
			}
			if tt.ok && err != nil {
				t.Fatalf("err = %v, want none", err)
			}
			if !tt.ok {
				var tooLarge *storage.TooLargeError
				if !errors.Is(err, storage.ErrTooLarge) || !errors.As(err, &tooLarge) || tooLarge.Max != 100 {
					t.Fatalf("err = %v, want ErrTooLarge with Max 100", err)
				}
			}
		})
	}
}

func TestInspectRefusesADisallowedSniffedTypeBeforeAnythingIsRead(t *testing.T) {
	opts := storage.PutOptions{MaxSize: 1 << 20, AllowedTypes: []string{"image/png", "application/pdf"}}
	_, err := storage.Inspect(strings.NewReader("<html>not an image</html>"), opts)
	var refused *storage.TypeNotAllowedError
	if !errors.Is(err, storage.ErrTypeNotAllowed) || !errors.As(err, &refused) || !strings.HasPrefix(refused.Type, "text/html") {
		t.Fatalf("err = %v, want ErrTypeNotAllowed naming text/html", err)
	}
	if _, err := storage.Inspect(bytes.NewReader(pngHeader), opts); err != nil {
		t.Fatalf("png refused: %v", err)
	}
}

func TestAllowedTypesMatchTheMediaTypeAndWildcards(t *testing.T) {
	for _, tt := range []struct {
		allowed []string
		ok      bool
	}{
		{nil, true},
		{[]string{"image/png"}, true},
		{[]string{"image/*"}, true},
		{[]string{"IMAGE/PNG"}, true},
		{[]string{"image/jpeg"}, false},
		{[]string{"application/*"}, false},
	} {
		_, err := storage.Inspect(bytes.NewReader(pngHeader), storage.PutOptions{MaxSize: 1024, AllowedTypes: tt.allowed})
		if (err == nil) != tt.ok {
			t.Errorf("AllowedTypes %v: err = %v, want ok=%v", tt.allowed, err, tt.ok)
		}
	}
}

func TestInspectNeedsAPositiveCap(t *testing.T) {
	for _, max := range []int64{0, -1} {
		if _, err := storage.Inspect(strings.NewReader("x"), storage.PutOptions{MaxSize: max}); !errors.Is(err, storage.ErrInvalidOptions) {
			t.Errorf("MaxSize %d: err = %v, want ErrInvalidOptions", max, err)
		}
	}
}

func TestInspectPassesAReaderErrorThrough(t *testing.T) {
	boom := errors.New("connection reset")
	in, err := storage.Inspect(io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(boom)), storage.PutOptions{MaxSize: 1024})
	if err == nil {
		_, err = io.Copy(io.Discard, in)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestInspectHandlesAnEmptyStream(t *testing.T) {
	in, err := storage.Inspect(strings.NewReader(""), storage.PutOptions{MaxSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, in); err != nil {
		t.Fatal(err)
	}
	if obj := in.Object("k"); obj.Size != 0 || obj.ContentType == "" {
		t.Fatalf("Object = %+v", obj)
	}
}

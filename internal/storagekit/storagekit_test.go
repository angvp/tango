package storagekit_test

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

func TestCheckRefusesACancelledContextBeforeAnInvalidKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := storagekit.Check(ctx, "not a key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled + invalid key = %v, want context.Canceled", err)
	}
	if err := storagekit.Check(context.Background(), "not a key"); !errors.Is(err, storage.ErrInvalidKey) {
		t.Fatalf("invalid key = %v, want ErrInvalidKey", err)
	}
	if err := storagekit.Check(context.Background(), storage.NewKey()); err != nil {
		t.Fatalf("valid key = %v", err)
	}
}

func TestRangeResolvesEveryEdgeWithoutOverflow(t *testing.T) {
	tests := []struct {
		name                 string
		size, offset, length int64
		end                  int64
		err                  error
	}{
		{"whole object", 10, 0, 10, 10, nil},
		{"negative length reads to the end", 10, 3, -1, 10, nil},
		{"MaxInt64 length reads to the end", 10, 3, math.MaxInt64, 10, nil},
		{"length past the end is clamped", 10, 8, 50, 10, nil},
		{"a middle slice", 10, 2, 3, 5, nil},
		{"offset at the end is an empty read", 10, 10, 4, 10, nil},
		{"empty object", 0, 0, 5, 0, nil},
		{"negative offset", 10, -1, 1, 0, storage.ErrInvalidRange},
		{"offset beyond the end", 10, 11, 1, 0, storage.ErrInvalidRange},
	}
	for _, tt := range tests {
		end, err := storagekit.Range(tt.size, tt.offset, tt.length)
		if !errors.Is(err, tt.err) || end != tt.end {
			t.Errorf("%s: Range(%d,%d,%d) = %d, %v; want %d, %v", tt.name, tt.size, tt.offset, tt.length, end, err, tt.end, tt.err)
		}
	}
}

func TestContextReaderStopsAsSoonAsTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &storagekit.ContextReader{Ctx: ctx, R: strings.NewReader("abcdef")}
	buf := make([]byte, 3)
	if n, err := r.Read(buf); n != 3 || err != nil {
		t.Fatalf("first Read = %d, %v", n, err)
	}
	cancel()
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("Read after cancel = %d, %v; want 0, context.Canceled", n, err)
	}
	if _, err := io.ReadAll(&storagekit.ContextReader{Ctx: context.Background(), R: strings.NewReader("xy")}); err != nil {
		t.Fatal(err)
	}
}

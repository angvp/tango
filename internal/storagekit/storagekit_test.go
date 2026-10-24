package storagekit_test

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
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

func TestSpoolWritesTheStreamAndReleaseRemovesIt(t *testing.T) {
	dir := t.TempDir()
	f, err := storagekit.Spool(context.Background(), "pfx", dir, "spool-*", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("Spool = %v", err)
	}
	got, err := os.ReadFile(f.Name())
	if err != nil || string(got) != "payload" {
		t.Fatalf("spooled bytes = %q, %v; want \"payload\"", got, err)
	}
	if err := storagekit.ReleaseSpool("pfx", f); err != nil {
		t.Fatalf("ReleaseSpool on a healthy file = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("spool left %d file(s) behind", len(entries))
	}
}

func TestSpoolWrapsACopyFailureAndLeavesNothingBehind(t *testing.T) {
	sentinel := errors.New("source failed")
	dir := t.TempDir()
	_, err := storagekit.Spool(context.Background(), "storage/x", dir, "spool-*", io.MultiReader(strings.NewReader("ab"), failingReader{sentinel}))
	if !errors.Is(err, sentinel) || !strings.HasPrefix(err.Error(), "storage/x: spool: ") {
		t.Fatalf("Spool = %v; want storage/x: spool: wrapping the source error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed spool left %d file(s) behind", len(entries))
	}
}

func TestSpoolRefusesACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := storagekit.Spool(ctx, "pfx", t.TempDir(), "spool-*", strings.NewReader("payload"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Spool with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestReleaseSpoolReportsACloseFailureAndStillRemoves(t *testing.T) {
	f, err := storagekit.Spool(context.Background(), "pfx", t.TempDir(), "spool-*", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	err = storagekit.ReleaseSpool("storage/x", f)
	if !errors.Is(err, os.ErrClosed) || !strings.HasPrefix(err.Error(), "storage/x: spool close: ") {
		t.Fatalf("ReleaseSpool on a closed file = %v; want storage/x: spool close: wrapping os.ErrClosed", err)
	}
	if _, statErr := os.Stat(f.Name()); !os.IsNotExist(statErr) {
		t.Fatalf("spool file still present after ReleaseSpool: %v", statErr)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestNormalizePrefixAcceptsOnlyPathsThatStayInside(t *testing.T) {
	tests := []struct {
		in, want string
		bad      bool
	}{
		{in: "", want: ""},
		{in: "/", want: ""},
		{in: "a/b", want: "a/b/"},
		{in: "a/b/", want: "a/b/"},
		{in: "a.b_c-D9", want: "a.b_c-D9/"},
		{in: "a//b", bad: true},
		{in: "a/./b", bad: true},
		{in: "a/../b", bad: true},
		{in: "/a", bad: true},
		{in: "a b", bad: true},
		{in: "a%2F", bad: true},
	}
	for _, tt := range tests {
		got, err := storagekit.NormalizePrefix(tt.in)
		if tt.bad {
			if err == nil {
				t.Errorf("NormalizePrefix(%q) = %q, want an error", tt.in, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("NormalizePrefix(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestValidSHA256IsExactly64LowercaseHex(t *testing.T) {
	const hello = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	tests := []struct {
		in   string
		want bool
	}{
		{hello, true},
		{strings.Repeat("0", 64), true},
		{"", false},
		{hello[:63], false},
		{hello + "0", false},
		{strings.ToUpper(hello), false},
		{hello[:63] + "g", false},
	}
	for _, tt := range tests {
		if got := storagekit.ValidSHA256(tt.in); got != tt.want {
			t.Errorf("ValidSHA256(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

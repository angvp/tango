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

func TestSpoolFileCopyFailureNamesTheStageAndLeavesNothingBehind(t *testing.T) {
	sentinel := errors.New("source failed")
	dir := t.TempDir()
	sp, err := storagekit.NewSpoolFile("storage/x", dir, "spool-*", storagekit.FileOps{})
	if err != nil {
		t.Fatal(err)
	}
	err = sp.Fill(context.Background(), io.MultiReader(strings.NewReader("ab"), failingReader{sentinel}))
	if !errors.Is(err, sentinel) || !strings.HasPrefix(err.Error(), "storage/x: spool: ") {
		t.Fatalf("Fill = %v; want storage/x: spool: wrapping the source error", err)
	}
	if err := sp.Release(); err != nil {
		t.Fatalf("Release after a failed copy = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed spool left %d file(s) behind", len(entries))
	}
}

func TestSpoolFileReleaseReportsACloseFailureAndStillRemoves(t *testing.T) {
	dir := t.TempDir()
	sp, err := storagekit.NewSpoolFile("storage/x", dir, "spool-*", storagekit.FileOps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Fill(context.Background(), strings.NewReader("payload")); err != nil {
		t.Fatal(err)
	}
	f, err := sp.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	err = sp.Release()
	if !errors.Is(err, os.ErrClosed) || !strings.HasPrefix(err.Error(), "storage/x: spool release: ") {
		t.Fatalf("Release with a closed handle = %v; want storage/x: spool release: wrapping os.ErrClosed", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("spool left %d file(s) behind after a failed release", len(entries))
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

var errInjected = errors.New("test: injected spool failure")

// syncFails and closeFails are FileOps that fail one operation and still
// perform it, so no handle or file leaks in the test.
func syncFails() storagekit.FileOps {
	return storagekit.FileOps{Sync: func(f *os.File) error { _ = f.Sync(); return errInjected }}
}

func closeFails() storagekit.FileOps {
	return storagekit.FileOps{Close: func(f *os.File) error { _ = f.Close(); return errInjected }}
}

func TestSpoolFileFillOpenReleaseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sp, err := storagekit.NewSpoolFile("pfx", dir, "spool-*", storagekit.FileOps{})
	if err != nil {
		t.Fatalf("NewSpoolFile = %v", err)
	}
	if err := sp.Fill(context.Background(), strings.NewReader("payload")); err != nil {
		t.Fatalf("Fill = %v", err)
	}
	f, err := sp.Open()
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	got, err := io.ReadAll(f)
	if err != nil || string(got) != "payload" {
		t.Fatalf("read back %q, %v; want payload", got, err)
	}
	if err := sp.Release(); err != nil {
		t.Fatalf("Release on a healthy spool = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("%d files left after Release", len(entries))
	}
}

func TestSpoolFileSyncFailureIsReportedByFill(t *testing.T) {
	sp, err := storagekit.NewSpoolFile("pfx", t.TempDir(), "spool-*", syncFails())
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Fill(context.Background(), strings.NewReader("payload")); !errors.Is(err, errInjected) {
		t.Fatalf("Fill = %v; want the sync failure", err)
	}
}

func TestSpoolFileCloseFailureIsReportedByFill(t *testing.T) {
	sp, err := storagekit.NewSpoolFile("pfx", t.TempDir(), "spool-*", closeFails())
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Fill(context.Background(), strings.NewReader("payload")); !errors.Is(err, errInjected) {
		t.Fatalf("Fill = %v; want the close failure", err)
	}
}

func TestSpoolFileReleaseRemovesTheFileAfterAFailedFill(t *testing.T) {
	dir := t.TempDir()
	sp, err := storagekit.NewSpoolFile("pfx", dir, "spool-*", closeFails())
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Fill(context.Background(), strings.NewReader("payload")); err == nil {
		t.Fatal("Fill succeeded; the close failure must surface")
	}
	if err := sp.Release(); err != nil {
		t.Logf("Release after a failed Fill = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("%d files left after Release of a failed spool", len(entries))
	}
}

func TestSpoolFileRefusesACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sp, err := storagekit.NewSpoolFile("pfx", t.TempDir(), "spool-*", storagekit.FileOps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Fill(ctx, strings.NewReader("payload")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fill = %v; want context.Canceled", err)
	}
}

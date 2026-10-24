package s3_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/angvp/tango/storage"
)

var errSourceRead = errors.New("test: source read failed")

type brokenSource struct{ r io.Reader }

func (b *brokenSource) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, errSourceRead
	}
	return n, err
}

// A source failure during the spool copy is returned with the package and the
// stage named, and keeps its own error reachable.
func TestAFailedSourceReadNamesTheSpoolStage(t *testing.T) {
	_, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	_, err := s.Put(context.Background(), &brokenSource{r: bytes.NewReader(bytes.Repeat([]byte("a"), 1024))}, storage.PutOptions{MaxSize: 4096})
	if !errors.Is(err, errSourceRead) || !strings.HasPrefix(err.Error(), "storage/s3: spool: ") {
		t.Fatalf("Put = %v; want storage/s3: spool: wrapping the source error", err)
	}
}

// An input over MaxSize is still a *storage.TooLargeError after the spool
// wraps it.
func TestAnOversizeInputStaysATooLargeError(t *testing.T) {
	_, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	_, err := s.Put(context.Background(), bytes.NewReader(bytes.Repeat([]byte("a"), 2000)), storage.PutOptions{MaxSize: 1000})
	var tooLarge *storage.TooLargeError
	if !errors.Is(err, storage.ErrTooLarge) || !errors.As(err, &tooLarge) || tooLarge.Max != 1000 {
		t.Fatalf("Put = %v; want ErrTooLarge with Max 1000", err)
	}
}

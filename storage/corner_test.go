package storage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storagetest"
)

func TestTheTypedErrorsNameWhatWasRefusedAndMatchTheirSentinels(t *testing.T) {
	tooLarge := error(&storage.TooLargeError{Max: 1234})
	if !errors.Is(tooLarge, storage.ErrTooLarge) || !strings.Contains(tooLarge.Error(), "1234") {
		t.Fatalf("TooLargeError = %q", tooLarge)
	}
	notAllowed := error(&storage.TypeNotAllowedError{Type: "text/html"})
	if !errors.Is(notAllowed, storage.ErrTypeNotAllowed) || !strings.Contains(notAllowed.Error(), "text/html") {
		t.Fatalf("TypeNotAllowedError = %q", notAllowed)
	}
	if errors.Is(tooLarge, storage.ErrTypeNotAllowed) || errors.Is(notAllowed, storage.ErrTooLarge) {
		t.Fatal("a typed error matched the wrong sentinel")
	}
}

func TestInspectorErrorsAreStickyAfterASourceFailureOrTheSizeCap(t *testing.T) {
	boom := errors.New("disk on fire")
	source := io.MultiReader(strings.NewReader("hello, plain text"), iotest.ErrReader(boom))
	in, err := storage.Inspect(source, storage.PutOptions{MaxSize: 1 << 20})
	if err == nil {
		_, err = io.ReadAll(in)
		if _, again := in.Read(make([]byte, 8)); !errors.Is(again, boom) {
			t.Fatalf("second Read = %v, want the same source error", again)
		}
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the source error", err)
	}

	capped, err := storage.Inspect(strings.NewReader(strings.Repeat("x", 100)), storage.PutOptions{MaxSize: 10})
	if err == nil {
		_, err = io.ReadAll(capped)
		if _, again := capped.Read(make([]byte, 8)); !errors.Is(again, storage.ErrTooLarge) {
			t.Fatalf("Read after the cap = %v, want ErrTooLarge again", again)
		}
	}
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestInspectRefusesATypeThatIsNotListedAndAcceptsAWildcard(t *testing.T) {
	text := "plain words, nothing else"
	if _, err := storage.Inspect(strings.NewReader(text), storage.PutOptions{MaxSize: 100, AllowedTypes: []string{"image/*", " IMAGE/png "}}); !errors.Is(err, storage.ErrTypeNotAllowed) {
		t.Fatalf("text allowed by an image list: %v", err)
	}
	if _, err := storage.Inspect(strings.NewReader(text), storage.PutOptions{MaxSize: 100, AllowedTypes: []string{"TEXT/*"}}); err != nil {
		t.Fatalf("text refused by text/*: %v", err)
	}
}

func TestServeIgnoresRangesItCannotUseAndRefusesOnesPastTheEnd(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	tests := []struct {
		header string
		status int
	}{
		{"bytes=abc-", http.StatusOK},
		{"bytes=-", http.StatusOK},
		{"bytes=-x", http.StatusOK},
		{"bytes=5-2", http.StatusOK},
		{"bytes=1-x", http.StatusOK},
		{"bytes=7", http.StatusOK},
		{"items=0-1", http.StatusOK},
		{"bytes=0-1,3-4", http.StatusOK},
		{"bytes=-0", http.StatusRequestedRangeNotSatisfiable},
		{"bytes=9999-", http.StatusRequestedRangeNotSatisfiable},
		{"bytes=-99999", http.StatusPartialContent},
	}
	for _, tt := range tests {
		rec := serve(t, store, obj.Key, storage.ServeOptions{}, http.MethodGet, map[string]string{"Range": tt.header})
		if rec.Code != tt.status {
			t.Errorf("Range %q: status %d, want %d", tt.header, rec.Code, tt.status)
		}
	}
}

func TestServeDefaultsAnUnknownTypeAndReducesOddFilenames(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)

	rec := serve(t, typeless{store}, obj.Key, storage.ServeOptions{}, http.MethodGet, nil)
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	for _, name := range []string{"..", ".", "  ", "dir/.."} {
		rec = serve(t, store, obj.Key, storage.ServeOptions{Filename: name}, http.MethodGet, nil)
		if got := rec.Header().Get("Content-Disposition"); got != "attachment" {
			t.Errorf("Filename %q gave Content-Disposition %q, want a bare attachment", name, got)
		}
	}
	long := strings.Repeat("é", 500) + ".png"
	rec = serve(t, store, obj.Key, storage.ServeOptions{Filename: long}, http.MethodGet, nil)
	if got := rec.Header().Get("Content-Disposition"); strings.Count(got, "%C3%A9") != 200 {
		t.Fatalf("a 504-rune name was not cut to 200 runes: %q", got)
	}
}

// typeless is a Store whose objects carry no content type.
type typeless struct{ storage.Store }

func (s typeless) Stat(ctx context.Context, key string) (storage.Info, error) {
	info, err := s.Store.Stat(ctx, key)
	info.ContentType = ""
	return info, err
}

// openFails passes Stat through and fails Open with a non-NotFound error.
type openFails struct {
	storage.Store
	err error
}

func (s openFails) Open(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, s.err
}

// brokenBody opens to a reader that fails after its first bytes.
type brokenBody struct{ storage.Store }

func (brokenBody) Open(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(strings.NewReader("ab"), iotest.ErrReader(errors.New("link down")))), nil
}

func TestServeReturnsAnOpenOrCopyFailureForTheFrameworkToLog(t *testing.T) {
	store := storagetest.NewMemory()
	obj := putObject(t, store, serveBody)
	boom := errors.New("backend down")

	err := storage.Serve(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), openFails{store, boom}, obj.Key, storage.ServeOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("Open failure = %v, want %v", err, boom)
	}
	err = storage.Serve(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), brokenBody{store}, obj.Key, storage.ServeOptions{})
	if err == nil || !strings.Contains(err.Error(), "link down") {
		t.Fatalf("copy failure = %v, want the read error", err)
	}
}

func TestUploadMapsABodyLimitHitInAFormFieldToTooLarge(t *testing.T) {
	body, contentType := multipartBody(t,
		part{field: "note", body: []byte(strings.Repeat("n", 4096))},
		part{field: "file", filename: "a.png", body: png},
	)
	req := httptest.NewRequest(http.MethodPost, "/upload/", body)
	req.Header.Set("Content-Type", contentType)
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 512)

	store := storagetest.NewMemory()
	_, err := storage.Upload(store, req, cap1M)
	var tooLarge *storage.TooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Max != 512 {
		t.Fatalf("err = %v, want TooLargeError{Max: 512}", err)
	}
}

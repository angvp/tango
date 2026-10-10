package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"
	"github.com/angvp/tango/storagetest"
)

var png = append(append([]byte{}, pngHeader...), bytes.Repeat([]byte("p"), 2000)...)

type part struct {
	field, filename, contentType string
	body                         []byte
}

func multipartBody(t testing.TB, parts ...part) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		disposition := fmt.Sprintf(`form-data; name=%q`, p.field)
		if p.filename != "" {
			disposition += `; filename="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p.filename) + `"`
		}
		h.Set("Content-Disposition", disposition)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		pw, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		pw.Write(p.body)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

// routeHandler serves POST /upload/ with view behind the given route
// middleware and global middleware.
func routeHandler(t *testing.T, view tango.View, global []tango.Middleware, route ...tango.Middleware) http.Handler {
	t.Helper()
	registry, err := tango.BuildRegistry(tango.Config{
		Middleware: global,
		InstalledApps: []tango.App{tango.NewApp("up", func(r *tango.Registry) error {
			return r.Routes().Include("/", tango.URLs{tango.Path(http.MethodPost, "/upload/", view, tango.Use(route...))})
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	h, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// uploadView is what a host would write: Upload, then its own mapping of
// the outcome to a status.
func uploadView(store storage.Store, opts storage.UploadOptions) tango.View {
	return func(ctx *tango.Context) error {
		file, err := storage.Upload(store, ctx.Request(), opts)
		switch {
		case errors.Is(err, storage.ErrTooLarge):
			return ctx.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "too large"})
		case errors.Is(err, storage.ErrTypeNotAllowed):
			return ctx.JSON(http.StatusUnsupportedMediaType, map[string]string{"error": "type"})
		case err != nil:
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return ctx.JSON(http.StatusCreated, map[string]any{
			"key": file.Key, "size": file.Size, "sha256": file.SHA256, "type": file.ContentType,
			"filename": file.Filename, "declared": file.DeclaredType,
		})
	}
}

func post(h http.Handler, body io.Reader, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/upload/", body)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func upload(t *testing.T, h http.Handler, parts ...part) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, parts...)
	return post(h, body, ct)
}

var cap1M = storage.UploadOptions{PutOptions: storage.PutOptions{MaxSize: 1 << 20}}

func mustOpen(t *testing.T, s storage.Store, key string) io.Reader {
	t.Helper()
	rc, err := s.Open(context.Background(), key, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rc.Close() })
	return rc
}

func fixedKeyStore() (storage.Store, string) {
	key := storage.NewKey()
	return storagetest.NewMemoryWithKeys(func() string { return key }), key
}

func notStored(t *testing.T, store storage.Store, key string) {
	t.Helper()
	if _, err := store.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("something was left stored: Stat = %v", err)
	}
}

func TestUploadStoresTheFileAndReturnsWhatTheHostKeeps(t *testing.T) {
	store := storagetest.NewMemory()
	w := upload(t, routeHandler(t, uploadView(store, cap1M), nil), part{"file", "me.png", "text/plain", png})
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{fmt.Sprintf(`"size":%d`, len(png)), `"type":"image/png"`, `"filename":"me.png"`, `"declared":"text/plain"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response %s lacks %s", body, want)
		}
	}
	key := body[strings.Index(body, `"key":"`)+7:][:32]
	if got, err := io.ReadAll(mustOpen(t, store, key)); err != nil || !bytes.Equal(got, png) {
		t.Fatalf("stored bytes differ (%d, %v)", len(got), err)
	}
}

func TestUploadHonoursTheConfiguredFieldAndIgnoresOtherFormFields(t *testing.T) {
	opts := storage.UploadOptions{Field: "avatar", PutOptions: storage.PutOptions{MaxSize: 1 << 20}}
	w := upload(t, routeHandler(t, uploadView(storagetest.NewMemory(), opts), nil),
		part{field: "csrf", body: []byte("token")},
		part{"avatar", "a.png", "image/png", png},
		part{field: "caption", body: []byte("hello")})
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

func TestUploadOverTheCapIsTooLargeAndStoresNothing(t *testing.T) {
	store, key := fixedKeyStore()
	opts := storage.UploadOptions{PutOptions: storage.PutOptions{MaxSize: 1000}}
	w := upload(t, routeHandler(t, uploadView(store, opts), nil), part{"file", "big.bin", "", bytes.Repeat([]byte("a"), 5000)})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	notStored(t, store, key)
}

func TestUploadJudgesTheSniffedTypeNotTheDeclaredOne(t *testing.T) {
	store, key := fixedKeyStore()
	opts := storage.UploadOptions{PutOptions: storage.PutOptions{MaxSize: 1 << 20, AllowedTypes: []string{"image/*"}}}
	w := upload(t, routeHandler(t, uploadView(store, opts), nil), part{"file", "x.png", "image/png", []byte("<html><script>1</script></html>")})
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	notStored(t, store, key)
}

func TestUploadRefusesRequestsThatAreNotOneFile(t *testing.T) {
	many := []part{}
	for i := 0; i < 40; i++ {
		many = append(many, part{field: fmt.Sprintf("f%d", i), body: []byte("x")})
	}
	cases := map[string]struct {
		parts []part
		want  error
	}{
		"no file part":      {[]part{{field: "caption", body: []byte("x")}}, storage.ErrNoFile},
		"the wrong field":   {[]part{{"other", "a.png", "", png}}, storage.ErrNoFile},
		"two files":         {[]part{{"file", "a.png", "", png}, {"file", "b.png", "", png}}, storage.ErrMultipleFiles},
		"two fields, files": {[]part{{"file", "a.png", "", png}, {"second", "b.png", "", png}}, storage.ErrMultipleFiles},
		"too many parts":    {append(many, part{"file", "a.png", "", png}), storage.ErrTooManyParts},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			store, key := fixedKeyStore()
			body, ct := multipartBody(t, tt.parts...)
			r := httptest.NewRequest(http.MethodPost, "/upload/", body)
			r.Header.Set("Content-Type", ct)
			if _, err := storage.Upload(store, r, cap1M); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			notStored(t, store, key)
		})
	}
}

func TestUploadRefusesANonMultipartBody(t *testing.T) {
	for _, ct := range []string{"application/json", "", "multipart/form-data"} {
		r := httptest.NewRequest(http.MethodPost, "/upload/", strings.NewReader(`{"a":1}`))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		if _, err := storage.Upload(storagetest.NewMemory(), r, cap1M); !errors.Is(err, storage.ErrNotMultipart) {
			t.Errorf("Content-Type %q: err = %v, want ErrNotMultipart", ct, err)
		}
	}
}

func TestTheClientFilenameIsOnlyEverASafeDisplayName(t *testing.T) {
	for name, tt := range map[string]struct{ in, want string }{
		"plain":              {"report.pdf", "report.pdf"},
		"a unix path":        {"../../etc/passwd", "passwd"},
		"a windows path":     {`C:\Users\x\evil.png`, "evil.png"},
		"control characters": {"a\u0085b\u202ec.png", "abc.png"},
		"unicode":            {"café.png", "café.png"},
		"only separators":    {"../", ""},
		"very long":          {strings.Repeat("n", 400) + ".png", strings.Repeat("n", 255)},
	} {
		t.Run(name, func(t *testing.T) {
			body, ct := multipartBody(t, part{"file", tt.in, "", png})
			r := httptest.NewRequest(http.MethodPost, "/upload/", body)
			r.Header.Set("Content-Type", ct)
			file, err := storage.Upload(storagetest.NewMemory(), r, cap1M)
			if err != nil {
				t.Fatal(err)
			}
			if file.Filename != tt.want {
				t.Fatalf("Filename = %q, want %q", file.Filename, tt.want)
			}
			if file.Filename != "" && strings.Contains(file.Key, file.Filename) {
				t.Fatalf("the filename leaked into the key %q", file.Key)
			}
		})
	}
}

func TestBodyLimitTopology(t *testing.T) {
	small := tango.MaxBodySize(1 << 10)
	big := tango.MaxBodySize(1 << 20)
	file := part{"file", "a.png", "", png} // about 2 KiB of body
	view := uploadView(storagetest.NewMemory(), cap1M)

	// An upload route with its own larger limit works when nothing stricter wraps it.
	if w := upload(t, routeHandler(t, view, nil, big), file); w.Code != http.StatusCreated {
		t.Fatalf("upload route with its own larger limit: %d %s", w.Code, w.Body)
	}
	// A stricter global limit cannot be raised by the route, which is why upload routes
	// sit outside it, in their own group.
	if w := upload(t, routeHandler(t, view, []tango.Middleware{small}, big), file); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a strict global limit must still win: %d", w.Code)
	}
	// A small limit on a JSON route keeps refusing uploads sent to it.
	if w := upload(t, routeHandler(t, view, nil, small), file); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a small route limit must refuse the upload: %d", w.Code)
	}
}

// countingReader counts how much of the request the server actually read.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// offerFile streams a multipart request with one file of size bytes.
func offerFile(size int) (*io.PipeReader, string) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, _ := mw.CreateFormFile("file", "big.bin")
		chunk := bytes.Repeat([]byte("z"), 1<<16)
		for written := 0; written < size; written += len(chunk) {
			if _, err := part.Write(chunk); err != nil {
				return
			}
		}
		mw.Close()
		pw.Close()
	}()
	return pr, mw.FormDataContentType()
}

func TestAnOversizedUploadIsRefusedWithoutReadingItAll(t *testing.T) {
	pr, ct := offerFile(64 << 20)
	counted := &countingReader{r: pr}
	r := httptest.NewRequest(http.MethodPost, "/upload/", counted)
	r.Header.Set("Content-Type", ct)
	_, err := storage.Upload(storagetest.NewMemory(), r, storage.UploadOptions{PutOptions: storage.PutOptions{MaxSize: 1 << 20}})
	pr.CloseWithError(io.ErrClosedPipe)
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if counted.n > 4<<20 {
		t.Fatalf("read %d bytes of a 64 MiB upload before refusing a 1 MiB cap", counted.n)
	}
}

func TestAnUploadIsStreamedNotBuffered(t *testing.T) {
	store, err := local.New(filepath.Join(t.TempDir(), "up"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const size = 48 << 20
	pr, ct := offerFile(size)
	r := httptest.NewRequest(http.MethodPost, "/upload/", pr)
	r.Header.Set("Content-Type", ct)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	file, err := storage.Upload(store, r, storage.UploadOptions{PutOptions: storage.PutOptions{MaxSize: 64 << 20}})
	runtime.ReadMemStats(&after)
	if err != nil || file.Size != size {
		t.Fatalf("Upload = %+v, %v", file, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 24<<20 {
		t.Fatalf("allocated %d MiB uploading %d MiB: the upload was buffered", allocated>>20, size>>20)
	}
}

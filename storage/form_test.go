package storage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"
	"github.com/angvp/tango/storagetest"
)

// recordingStore counts what a form read does to a Store.
type recordingStore struct {
	storage.Store
	mu      sync.Mutex
	puts    int
	deleted []string
	// afterPut runs after each successful Put.
	afterPut func()
	// deleteSawDeadContext is set when a Delete ran with a finished context.
	deleteSawDeadContext bool
}

func (s *recordingStore) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (storage.Object, error) {
	s.mu.Lock()
	s.puts++
	s.mu.Unlock()
	object, err := s.Store.Put(ctx, r, opts)
	if err == nil && s.afterPut != nil {
		s.afterPut()
	}
	return object, err
}

func (s *recordingStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, key)
	if ctx.Err() != nil {
		s.deleteSawDeadContext = true
	}
	s.mu.Unlock()
	return s.Store.Delete(ctx, key)
}

func newRecording() *recordingStore { return &recordingStore{Store: storagetest.NewMemory()} }

func formRequest(t *testing.T, parts ...part) *http.Request {
	t.Helper()
	body, contentType := multipartBody(t, parts...)
	request := httptest.NewRequest(http.MethodPost, "/form/", body)
	request.Header.Set("Content-Type", contentType)
	return request
}

var logoRules = storage.PutOptions{MaxSize: 1 << 20, AllowedTypes: []string{"image/png"}}

func TestReadFormReturnsTextValuesAndStoresTheDeclaredFile(t *testing.T) {
	store := newRecording()
	request := formRequest(t,
		part{field: "name", body: []byte("Acme")},
		part{field: "logo", filename: "acme.png", contentType: "image/png", body: png},
		part{field: "note", body: []byte("first")},
		part{field: "note", body: []byte("second")},
	)

	form, err := storage.ReadForm(store, request, storage.FormOptions{Files: map[string]storage.PutOptions{"logo": logoRules}})

	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	if got := form.Values.Get("name"); got != "Acme" {
		t.Fatalf("name = %q, want Acme", got)
	}
	if got := form.Values["note"]; len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("note = %q, want both values in order", got)
	}
	logo, ok := form.Files["logo"]
	if !ok || logo.Key == "" || logo.Filename != "acme.png" || logo.ContentType != "image/png" {
		t.Fatalf("logo = %+v, ok = %v, want a stored image/png named acme.png", logo, ok)
	}
	if info, err := store.Stat(context.Background(), logo.Key); err != nil || info.Size != int64(len(png)) {
		t.Fatalf("Stat(%q) = %+v, %v, want the stored bytes", logo.Key, info, err)
	}
	if store.puts != 1 || len(store.deleted) != 0 {
		t.Fatalf("puts = %d, deletes = %d, want 1 and 0", store.puts, len(store.deleted))
	}
}

func tokenRules(valid string) storage.FormOptions {
	return storage.FormOptions{
		Files:      map[string]storage.PutOptions{"logo": logoRules},
		TokenField: "csrf_token",
		CheckToken: func(value string) bool { return value == valid },
	}
}

func TestReadFormChecksTheTokenBeforeItReadsAnyFile(t *testing.T) {
	logo := part{field: "logo", filename: "acme.png", contentType: "image/png", body: png}
	tests := []struct {
		name  string
		parts []part
	}{
		{"no token at all", []part{{field: "name", body: []byte("Acme")}, logo}},
		{"a wrong token", []part{{field: "csrf_token", body: []byte("nope")}, logo}},
		{"an empty token", []part{{field: "csrf_token", body: nil}, logo}},
		{"a token after the file", []part{logo, {field: "csrf_token", body: []byte("good")}}},
		{"a token after another field", []part{{field: "name", body: []byte("Acme")}, {field: "csrf_token", body: []byte("good")}, logo}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRecording()

			form, err := storage.ReadForm(store, formRequest(t, tc.parts...), tokenRules("good"))

			if !errors.Is(err, storage.ErrFormToken) {
				t.Fatalf("ReadForm = %v, want ErrFormToken", err)
			}
			if len(form.Values) != 0 || len(form.Files) != 0 {
				t.Fatalf("form = %+v, want nothing returned with a bad token", form)
			}
			if store.puts != 0 {
				t.Fatalf("puts = %d, want 0: a request without a valid first token must not reach the store", store.puts)
			}
		})
	}

	t.Run("a good first token reads the form and is not returned as a value", func(t *testing.T) {
		store := newRecording()
		request := formRequest(t, part{field: "csrf_token", body: []byte("good")}, part{field: "name", body: []byte("Acme")}, logo)

		form, err := storage.ReadForm(store, request, tokenRules("good"))

		if err != nil {
			t.Fatalf("ReadForm: %v", err)
		}
		if _, leaked := form.Values["csrf_token"]; leaked || form.Values.Get("name") != "Acme" || form.Files["logo"].Key == "" {
			t.Fatalf("form = %+v, want name and logo without the token", form)
		}
	})

	t.Run("a token field with no check is a configuration error", func(t *testing.T) {
		request := formRequest(t, part{field: "csrf_token", body: []byte("good")})
		_, err := storage.ReadForm(newRecording(), request, storage.FormOptions{TokenField: "csrf_token"})
		if !errors.Is(err, storage.ErrInvalidOptions) {
			t.Fatalf("ReadForm = %v, want ErrInvalidOptions", err)
		}
	})
}

var twoLogos = map[string]storage.PutOptions{"logo": logoRules, "banner": logoRules}

func TestReadFormStoresEachDeclaredFileUnderItsOwnRules(t *testing.T) {
	store := newRecording()
	options := storage.FormOptions{Files: map[string]storage.PutOptions{
		"logo":   logoRules,
		"banner": {MaxSize: 1 << 20, AllowedTypes: []string{"image/*"}},
	}}
	request := formRequest(t,
		part{field: "logo", filename: "a.png", body: png},
		part{field: "banner", filename: "b.png", body: png},
	)

	form, err := storage.ReadForm(store, request, options)

	if err != nil || form.Files["logo"].Key == "" || form.Files["banner"].Key == "" || form.Files["logo"].Key == form.Files["banner"].Key {
		t.Fatalf("form = %+v, err = %v, want two distinct stored files", form.Files, err)
	}
}

func TestReadFormTreatsAnEmptyFileInputAsNoFile(t *testing.T) {
	store := newRecording()
	request := formRequest(t, part{field: "name", body: []byte("Acme")}, part{field: "logo", filename: "", body: nil})

	form, err := storage.ReadForm(store, request, storage.FormOptions{Files: twoLogos})

	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	if len(form.Files) != 0 || store.puts != 0 {
		t.Fatalf("files = %v, puts = %d, want no file and no Put", form.Files, store.puts)
	}
	if _, listed := form.Values["logo"]; listed {
		t.Fatal("the empty file input came back as a text value")
	}
}

func TestReadFormRefusalsLeaveNothingStored(t *testing.T) {
	good := part{field: "logo", filename: "a.png", body: png}
	oversized := part{field: "banner", filename: "big.png", body: append(append([]byte{}, pngHeader...), make([]byte, 2<<20)...)}
	notPNG := part{field: "banner", filename: "x.txt", body: []byte("plain text, not an image")}
	tests := []struct {
		name  string
		parts []part
		want  error
	}{
		{"a file in an undeclared field", []part{good, {field: "other", filename: "o.png", body: png}}, storage.ErrUnexpectedFile},
		{"a second file in one field", []part{good, {field: "logo", filename: "b.png", body: png}}, storage.ErrMultipleFiles},
		{"a file past its cap", []part{good, oversized}, storage.ErrTooLarge},
		{"a type its field does not allow", []part{good, notPNG}, storage.ErrTypeNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRecording()
			options := storage.FormOptions{Files: map[string]storage.PutOptions{"logo": logoRules, "banner": {MaxSize: 1 << 20, AllowedTypes: []string{"image/png"}}}}

			form, err := storage.ReadForm(store, formRequest(t, tc.parts...), options)

			if !errors.Is(err, tc.want) {
				t.Fatalf("ReadForm = %v, want %v", err, tc.want)
			}
			if len(form.Files) != 0 {
				t.Fatalf("files = %v, want none returned after a refusal", form.Files)
			}
			if store.puts == 0 {
				t.Fatal("the first file was never stored, so this case does not test cleanup")
			}
			if len(store.deleted) != store.puts-failedPuts(tc.want) {
				t.Fatalf("puts = %d, deletes = %d: every object the call stored must be deleted", store.puts, len(store.deleted))
			}
		})
	}
}

// failedPuts is how many Puts of a refusal itself failed (and so stored nothing).
func failedPuts(want error) int {
	if errors.Is(want, storage.ErrTooLarge) || errors.Is(want, storage.ErrTypeNotAllowed) {
		return 1
	}
	return 0
}

func TestReadFormCleansUpEvenWhenTheRequestContextIsDone(t *testing.T) {
	store := newRecording()
	request := formRequest(t, part{field: "logo", filename: "a.png", body: png}, part{field: "logo", filename: "b.png", body: png})
	ctx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(ctx)
	store.afterPut = cancel

	_, err := storage.ReadForm(store, request, storage.FormOptions{Files: twoLogos})

	if !errors.Is(err, storage.ErrMultipleFiles) {
		t.Fatalf("ReadForm = %v, want ErrMultipleFiles", err)
	}
	if len(store.deleted) != 1 || store.deleteSawDeadContext {
		t.Fatalf("deleted = %d, ran with a finished context = %v, want 1 delete that survives a canceled request", len(store.deleted), store.deleteSawDeadContext)
	}
}

func TestReadFormBoundsTheFormAndTheBody(t *testing.T) {
	t.Run("a text field past its bound", func(t *testing.T) {
		request := formRequest(t, part{field: "bio", body: make([]byte, 128<<10)})
		_, err := storage.ReadForm(newRecording(), request, storage.FormOptions{})
		if !errors.Is(err, storage.ErrFieldTooLarge) {
			t.Fatalf("ReadForm = %v, want ErrFieldTooLarge", err)
		}
	})
	t.Run("too many parts", func(t *testing.T) {
		parts := []part{{field: "logo", filename: "a.png", body: png}}
		for i := 0; i < 300; i++ {
			parts = append(parts, part{field: "f", body: []byte("x")})
		}
		store := newRecording()
		_, err := storage.ReadForm(store, formRequest(t, parts...), storage.FormOptions{Files: twoLogos})
		if !errors.Is(err, storage.ErrTooManyParts) || len(store.deleted) != 1 {
			t.Fatalf("ReadForm = %v, deletes = %d, want ErrTooManyParts and the stored file deleted", err, len(store.deleted))
		}
	})
	t.Run("a body that is not multipart", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/form/", nil)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := storage.ReadForm(newRecording(), request, storage.FormOptions{})
		if !errors.Is(err, storage.ErrNotMultipart) {
			t.Fatalf("ReadForm = %v, want ErrNotMultipart", err)
		}
	})
	t.Run("a body cut by a body limit", func(t *testing.T) {
		request := formRequest(t, part{field: "logo", filename: "a.png", body: png})
		request.Body = http.MaxBytesReader(httptest.NewRecorder(), request.Body, 500)
		_, err := storage.ReadForm(newRecording(), request, storage.FormOptions{Files: twoLogos})
		if !errors.Is(err, storage.ErrTooLarge) {
			t.Fatalf("ReadForm = %v, want ErrTooLarge", err)
		}
	})
}

func TestReadFormOnTheLocalStore(t *testing.T) {
	store, err := local.New(filepath.Join(t.TempDir(), "forms"))
	if err != nil {
		t.Fatal(err)
	}
	options := storage.FormOptions{Files: twoLogos, TokenField: "csrf_token", CheckToken: func(v string) bool { return v == "good" }}

	form, err := storage.ReadForm(store, formRequest(t,
		part{field: "csrf_token", body: []byte("good")},
		part{field: "name", body: []byte("Acme")},
		part{field: "logo", filename: "a.png", body: png},
	), options)
	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	if _, err := store.Stat(context.Background(), form.Files["logo"].Key); err != nil {
		t.Fatalf("the stored file is not on disk: %v", err)
	}

	_, err = storage.ReadForm(store, formRequest(t,
		part{field: "csrf_token", body: []byte("good")},
		part{field: "logo", filename: "a.png", body: png},
		part{field: "logo", filename: "b.png", body: png},
	), options)
	if !errors.Is(err, storage.ErrMultipleFiles) {
		t.Fatalf("ReadForm = %v, want ErrMultipleFiles", err)
	}
}

func TestReadFormRefusesDataThatHidesBehindNoFilename(t *testing.T) {
	store := newRecording()
	request := formRequest(t, part{field: "logo", filename: "", body: []byte("bytes with no filename")})

	_, err := storage.ReadForm(store, request, storage.FormOptions{Files: twoLogos})

	if !errors.Is(err, storage.ErrUnexpectedFile) || store.puts != 0 {
		t.Fatalf("ReadForm = %v, puts = %d, want ErrUnexpectedFile and no Put", err, store.puts)
	}
}

func TestReadFormNeverReturnsATokenFieldAfterTheFirstPart(t *testing.T) {
	for name, later := range map[string]part{
		"a repeated text token":  {field: "csrf_token", body: []byte("good")},
		"a token sent as a file": {field: "csrf_token", filename: "t.png", body: png},
	} {
		t.Run(name, func(t *testing.T) {
			store := newRecording()
			options := tokenRules("good")
			options.Files["csrf_token"] = logoRules
			request := formRequest(t, part{field: "csrf_token", body: []byte("good")}, later)

			form, err := storage.ReadForm(store, request, options)

			if !errors.Is(err, storage.ErrFormToken) || len(form.Values) != 0 || store.puts != 0 {
				t.Fatalf("ReadForm = %v, values = %v, puts = %d, want ErrFormToken and nothing returned or stored", err, form.Values, store.puts)
			}
		})
	}
}

func TestReadFormRefusesAnOversizedToken(t *testing.T) {
	store := newRecording()
	options := tokenRules(string(make([]byte, 64<<10)))
	request := formRequest(t, part{field: "csrf_token", body: make([]byte, 64<<10+1)}, part{field: "logo", filename: "a.png", body: png})

	_, err := storage.ReadForm(store, request, options)

	if !errors.Is(err, storage.ErrFormToken) || store.puts != 0 {
		t.Fatalf("ReadForm = %v, puts = %d, want ErrFormToken: a truncated token must not be accepted", err, store.puts)
	}
}

func TestReadFormBoundsTheTextFieldsTogether(t *testing.T) {
	var parts []part
	for i := 0; i < 20; i++ {
		parts = append(parts, part{field: "bio", body: make([]byte, 60<<10)})
	}

	_, err := storage.ReadForm(newRecording(), formRequest(t, parts...), storage.FormOptions{})

	if !errors.Is(err, storage.ErrFieldTooLarge) {
		t.Fatalf("ReadForm = %v, want ErrFieldTooLarge for 1.2 MiB of text in 20 fields", err)
	}
}

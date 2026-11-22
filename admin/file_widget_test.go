package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storagetest"
)

// fileClient is a model with one file field, held as the object's key.
type fileClient struct {
	ID   int64 `tango:"pk"`
	Name string
	Code string `tango:"unique"`
	Logo string
}

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

var csrfInput = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// fileAdmin builds an admin with fileClient's Logo as a file field and returns
// the handler, the database and a signed-in staff cookie.
func fileAdmin(t *testing.T, logo admin.Widget, options ...admin.Option) (http.Handler, *sql.DB, *http.Cookie) {
	t.Helper()
	handler, db, cookie, err := buildFileAdmin(t, nil, logo, options...)
	if err != nil {
		t.Fatalf("build the admin: %v", err)
	}
	return handler, db, cookie
}

// buildFileAdmin is fileAdmin with a hook on the registry before the admin is
// installed, and with the startup error returned instead of failing the test.
func buildFileAdmin(t *testing.T, setup func(*tango.Registry), logo admin.Widget, options ...admin.Option) (http.Handler, *sql.DB, *http.Cookie, error) {
	t.Helper()
	registry := tango.NewRegistry()
	if setup != nil {
		setup(registry)
	}
	if err := registry.Models().Register(fileClient{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(fileClient{}, admin.Options{Widgets: map[string]admin.Widget{"Logo": logo}}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	if err := admin.CreateAccount(context.Background(), store, "admin", "secret"); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := registry.Register(admin.New(store, options...)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		return nil, nil, nil, err
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB, loginAndGetSessionCookie(t, handler, "admin", "secret"), nil
}

type upload struct {
	field, filename string
	body            []byte
}

// submitFiles posts a multipart form with the CSRF token first, the text
// fields, then the files, as a browser does.
func submitFiles(t *testing.T, handler http.Handler, cookie *http.Cookie, path string, text [][2]string, files ...upload) *httptest.ResponseRecorder {
	t.Helper()
	page := performAdminRequest(handler, http.MethodGet, path, cookie)
	match := csrfInput.FindStringSubmatch(page.Body.String())
	if match == nil {
		t.Fatalf("no csrf token on %s: %s", path, page.Body.String())
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("csrf_token", match[1])
	for _, kv := range text {
		_ = writer.WriteField(kv[0], kv[1])
	}
	for _, f := range files {
		part, _ := writer.CreateFormFile(f.field, f.filename)
		_, _ = part.Write(f.body)
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func logoField(store storage.Store) admin.Widget {
	return admin.File(admin.FileOptions{
		Store: store,
		Put:   storage.PutOptions{MaxSize: 1 << 20, AllowedTypes: []string{"image/png"}},
		URL:   func(key string) string { return "https://cdn.test/" + key },
	})
}

func storedLogo(t *testing.T, db *sql.DB) string {
	t.Helper()
	var key string
	if err := db.QueryRow(`SELECT logo FROM file_client`).Scan(&key); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	return key
}

func TestAdminFileFieldUploadsOnCreateAndKeepsTheKey(t *testing.T) {
	store := storagetest.NewMemory()
	handler, db, cookie := fileAdmin(t, logoField(store))

	form := performAdminRequest(handler, http.MethodGet, "/admin/file_client/new/", cookie).Body.String()
	for _, want := range []string{`enctype="multipart/form-data"`, `type="file"`, `name="Logo"`, `name="Name"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("the form lacks %s: %s", want, form)
		}
	}

	response := submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "Acme"}, {"Code", "acme"}}, upload{"Logo", "logo.png", pngBytes})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want a redirect; body: %s", response.Code, response.Body.String())
	}
	key := storedLogo(t, db)
	if !storage.ValidKey(key) {
		t.Fatalf("the row holds %q, want the store's generated key", key)
	}
	if _, err := store.Stat(context.Background(), key); err != nil {
		t.Fatalf("the object is not in the store: %v", err)
	}
}

// recordingStore wraps the memory store to count and fail calls.
type recordingStore struct {
	storage.Store
	puts       int
	deleted    []string
	failDelete bool
	failPut    error
}

func newRecordingStore() *recordingStore { return &recordingStore{Store: storagetest.NewMemory()} }

func (s *recordingStore) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if s.failPut != nil {
		return storage.Object{}, s.failPut
	}
	object, err := s.Store.Put(ctx, r, opts)
	if err == nil {
		s.puts++
	}
	return object, err
}

func (s *recordingStore) Delete(ctx context.Context, key string) error {
	if s.failDelete {
		return errors.New("delete refused")
	}
	s.deleted = append(s.deleted, key)
	return s.Store.Delete(ctx, key)
}

func (s *recordingStore) has(t *testing.T, key string) bool {
	t.Helper()
	_, err := s.Store.Stat(context.Background(), key)
	return err == nil
}

// createWithLogo creates a row with a logo and returns its edit path and key.
func createWithLogo(t *testing.T, handler http.Handler, db *sql.DB, cookie *http.Cookie, code string) (path, key string) {
	t.Helper()
	response := submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "Acme"}, {"Code", code}}, upload{"Logo", "logo.png", pngBytes})
	if response.Code != http.StatusFound {
		t.Fatalf("create: status = %d; body: %s", response.Code, response.Body.String())
	}
	var id int64
	if err := db.QueryRow(`SELECT id, logo FROM file_client WHERE code = $1`, code).Scan(&id, &key); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	return fmt.Sprintf("/admin/file_client/%d/", id), key
}

func rowLogo(t *testing.T, db *sql.DB, code string) string {
	t.Helper()
	var key string
	if err := db.QueryRow(`SELECT logo FROM file_client WHERE code = $1`, code).Scan(&key); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	return key
}

func TestAdminFileFieldShowsTheCurrentFileWithTheHostsURLAndARemoveBox(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	path, key := createWithLogo(t, handler, db, cookie, "acme")

	form := performAdminRequest(handler, http.MethodGet, path, cookie).Body.String()

	for _, want := range []string{`href="https://cdn.test/` + key + `"`, `name="Logo__remove"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("the edit form lacks %s: %s", want, form)
		}
	}
	if strings.Contains(performAdminRequest(handler, http.MethodGet, "/admin/file_client/new/", cookie).Body.String(), "Logo__remove") {
		t.Fatal("a new row's form offers to remove a file it does not have")
	}
}

func TestAdminFileFieldKeepsTheFileWhenNoneIsChosen(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	path, key := createWithLogo(t, handler, db, cookie, "acme")

	response := submitFiles(t, handler, cookie, path, [][2]string{{"Name", "Acme 2"}, {"Code", "acme"}}, upload{"Logo", "", nil})

	if response.Code != http.StatusFound || rowLogo(t, db, "acme") != key || len(store.deleted) != 0 || !store.has(t, key) {
		t.Fatalf("status = %d, key = %q (was %q), deleted = %v, want the file kept", response.Code, rowLogo(t, db, "acme"), key, store.deleted)
	}
}

func TestAdminFileFieldReplacesTheFileAndDeletesTheOldOneAfterTheSave(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	path, old := createWithLogo(t, handler, db, cookie, "acme")

	response := submitFiles(t, handler, cookie, path, [][2]string{{"Name", "Acme"}, {"Code", "acme"}}, upload{"Logo", "new.png", pngBytes})

	fresh := rowLogo(t, db, "acme")
	if response.Code != http.StatusFound || fresh == old || !store.has(t, fresh) || store.has(t, old) {
		t.Fatalf("status = %d, key %q -> %q, want a new key, the new object stored and the old one deleted", response.Code, old, fresh)
	}
}

func TestAdminFileFieldRemoveClearsTheFieldAndDeletesTheObject(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	path, old := createWithLogo(t, handler, db, cookie, "acme")

	response := submitFiles(t, handler, cookie, path, [][2]string{{"Name", "Acme"}, {"Code", "acme"}, {"Logo__remove", "on"}})

	if response.Code != http.StatusFound || rowLogo(t, db, "acme") != "" || store.has(t, old) {
		t.Fatalf("status = %d, key = %q, object kept = %v, want the field cleared and the object deleted", response.Code, rowLogo(t, db, "acme"), store.has(t, old))
	}
}

func TestAdminFileFieldRefusesAFileAndKeepsWhatWasTyped(t *testing.T) {
	big := append(append([]byte{}, pngBytes...), make([]byte, 2<<20)...)
	tests := map[string]struct {
		file upload
		want string
	}{
		"too large":        {upload{"Logo", "big.png", big}, "limit is 1 MiB"},
		"type not allowed": {upload{"Logo", "x.txt", []byte("plain text, not an image")}, "not allowed"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store := newRecordingStore()
			handler, db, cookie := fileAdmin(t, logoField(store))

			response := submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "Typed name"}, {"Code", "typed"}}, tc.file)

			body := response.Body.String()
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(body, tc.want) || !strings.Contains(body, `value="Typed name"`) {
				t.Fatalf("status = %d, want 422 with %q and the typed name kept; body: %s", response.Code, tc.want, body)
			}
			var rows int
			_ = db.QueryRow(`SELECT count(*) FROM file_client`).Scan(&rows)
			if rows != 0 || store.puts != len(store.deleted) {
				t.Fatalf("rows = %d, puts = %d, deletes = %d, want no row and nothing left stored", rows, store.puts, len(store.deleted))
			}
		})
	}
}

func TestAdminFileFieldAFailedSaveDeletesTheNewObjectAndKeepsTheOldKey(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	_, _ = createWithLogo(t, handler, db, cookie, "first")
	path, old := createWithLogo(t, handler, db, cookie, "second")

	response := submitFiles(t, handler, cookie, path, [][2]string{{"Name", "Acme"}, {"Code", "first"}}, upload{"Logo", "new.png", pngBytes})

	if response.Code != http.StatusUnprocessableEntity || rowLogo(t, db, "second") != old || !store.has(t, old) {
		t.Fatalf("status = %d, key = %q (was %q), want 422 and the old key and object kept", response.Code, rowLogo(t, db, "second"), old)
	}
	if store.puts != 3 || len(store.deleted) != 1 {
		t.Fatalf("puts = %d, deletes = %d, want the new object stored and then deleted", store.puts, len(store.deleted))
	}
}

func TestAdminFileFieldAFailedDeleteOfTheOldFileDoesNotFailTheSave(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	path, old := createWithLogo(t, handler, db, cookie, "acme")
	store.failDelete = true

	response := submitFiles(t, handler, cookie, path, [][2]string{{"Name", "Acme"}, {"Code", "acme"}}, upload{"Logo", "new.png", pngBytes})

	if response.Code != http.StatusFound || rowLogo(t, db, "acme") == old {
		t.Fatalf("status = %d, key unchanged = %v, want the save to succeed", response.Code, rowLogo(t, db, "acme") == old)
	}
}

func TestAdminFileFieldAStoreFailureIsAGenericMessageAndSavesNothing(t *testing.T) {
	store := newRecordingStore()
	store.failPut = errors.New("bucket unreachable: secret-endpoint.internal")
	handler, db, cookie := fileAdmin(t, logoField(store))

	response := submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "Acme"}, {"Code", "acme"}}, upload{"Logo", "logo.png", pngBytes})

	body := response.Body.String()
	var rows int
	_ = db.QueryRow(`SELECT count(*) FROM file_client`).Scan(&rows)
	if response.Code != http.StatusUnprocessableEntity || rows != 0 || strings.Contains(body, "secret-endpoint") {
		t.Fatalf("status = %d, rows = %d, leaks the store's error = %v, want a generic 422 and no row", response.Code, rows, strings.Contains(body, "secret-endpoint"))
	}
}

func TestAdminFileFieldForgedFormStoresNothing(t *testing.T) {
	store := newRecordingStore()
	handler, _, cookie := fileAdmin(t, logoField(store))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("csrf_token", "forged")
	part, _ := writer.CreateFormFile("Logo", "logo.png")
	_, _ = part.Write(pngBytes)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/admin/file_client/new/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || store.puts != 0 {
		t.Fatalf("status = %d, puts = %d, want 403 and nothing stored", response.Code, store.puts)
	}
}

func TestAdminFileFieldASubmittedKeyIsNotTrusted(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))

	response := submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "Acme"}, {"Code", "acme"}, {"Logo", "someone-elses-key"}})

	var rows int
	_ = db.QueryRow(`SELECT count(*) FROM file_client`).Scan(&rows)
	if response.Code != http.StatusUnprocessableEntity || rows != 0 {
		t.Fatalf("status = %d, rows = %d, want a refusal: a key comes only from a stored upload", response.Code, rows)
	}
}

// sized is a model whose file field is not a string.
type sizedClient struct {
	ID   int64 `tango:"pk"`
	Logo int64
}

func TestAdminFileFieldIsRefusedAtStartup(t *testing.T) {
	good := storage.PutOptions{MaxSize: 1 << 20}
	tests := map[string]admin.Widget{
		"no size limit":      admin.File(admin.FileOptions{Store: storagetest.NewMemory()}),
		"a negative limit":   admin.File(admin.FileOptions{Store: storagetest.NewMemory(), Put: storage.PutOptions{MaxSize: -1}}),
		"a non-string field": nil,
	}
	for name, widget := range tests {
		t.Run(name, func(t *testing.T) {
			var err error
			if widget == nil {
				registry := tango.NewRegistry()
				_ = registry.Models().Register(sizedClient{})
				_ = registry.Admin().Register(sizedClient{}, admin.Options{Widgets: map[string]admin.Widget{"Logo": admin.File(admin.FileOptions{Store: storagetest.NewMemory(), Put: good})}})
				_, store := migratedAdminDB(t, registry)
				_ = registry.Register(admin.New(store))
				err = registry.RunRegistration()
			} else {
				_, _, _, err = buildFileAdmin(t, nil, widget)
			}
			if !errors.Is(err, admin.ErrInvalidFile) || !strings.Contains(err.Error(), "Logo") {
				t.Fatalf("startup = %v, want ErrInvalidFile naming the Logo field", err)
			}
		})
	}
}

func TestAdminFileFieldUsesTheRegistryStoreUnlessTheFieldHasItsOwn(t *testing.T) {
	put := storage.PutOptions{MaxSize: 1 << 20}
	shared, own := newRecordingStore(), newRecordingStore()
	for name, tc := range map[string]struct {
		widget    admin.Widget
		wantShare int
		wantOwn   int
	}{
		"registry store":  {admin.File(admin.FileOptions{Put: put}), 1, 0},
		"the field's own": {admin.File(admin.FileOptions{Put: put, Store: own}), 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			shared.puts, own.puts = 0, 0
			handler, _, cookie, err := buildFileAdmin(t, func(r *tango.Registry) {
				if err := r.SetStorage(shared); err != nil {
					t.Fatal(err)
				}
			}, tc.widget)
			if err != nil {
				t.Fatal(err)
			}

			submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "A"}, {"Code", "a"}}, upload{"Logo", "l.png", pngBytes})

			if shared.puts != tc.wantShare || own.puts != tc.wantOwn {
				t.Fatalf("registry store puts = %d, own puts = %d, want %d and %d", shared.puts, own.puts, tc.wantShare, tc.wantOwn)
			}
		})
	}
}

func TestAdminFileFieldFallsBackToALocalDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads")
	t.Setenv("TANGO_STORAGE_DIR", dir)
	handler, db, cookie := fileAdmin(t, admin.File(admin.FileOptions{Put: storage.PutOptions{MaxSize: 1 << 20}}))

	response := submitFiles(t, handler, cookie, "/admin/file_client/new/", [][2]string{{"Name", "A"}, {"Code", "a"}}, upload{"Logo", "l.png", pngBytes})

	key := rowLogo(t, db, "a")
	if response.Code != http.StatusFound || key == "" {
		t.Fatalf("status = %d, key = %q, want the upload saved", response.Code, key)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
		t.Fatalf("the local directory %s holds %d entries (err %v), want the stored file", dir, len(entries), err)
	}
}

func TestAdminFileFieldFailsAtStartupWhenTheLocalFallbackIsUnusable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGO_STORAGE_DIR", filepath.Join(blocker, "uploads"))

	_, _, _, err := buildFileAdmin(t, nil, admin.File(admin.FileOptions{Put: storage.PutOptions{MaxSize: 1 << 20}}))

	if !errors.Is(err, admin.ErrInvalidFile) || !strings.Contains(err.Error(), "Logo") {
		t.Fatalf("startup = %v, want ErrInvalidFile naming the Logo field", err)
	}
}

// publicStore is a store that knows where its objects are served.
type publicStore struct{ storage.Store }

func (publicStore) PublicURL(key string) (string, bool) { return "https://bucket.test/" + key, true }

func TestAdminFileFieldLinksThroughTheStoresPublicURLWhenTheHostGivesNone(t *testing.T) {
	store := publicStore{storagetest.NewMemory()}
	handler, db, cookie := fileAdmin(t, admin.File(admin.FileOptions{Store: store, Put: storage.PutOptions{MaxSize: 1 << 20}}))
	path, key := createWithLogo(t, handler, db, cookie, "acme")

	form := performAdminRequest(handler, http.MethodGet, path, cookie).Body.String()

	if !strings.Contains(form, `href="https://bucket.test/`+key+`"`) {
		t.Fatalf("the form does not link through the store's public URL: %s", form)
	}
}

func TestAdminFormWithoutAFileFieldStaysUrlencoded(t *testing.T) {
	registry := tango.NewRegistry()
	_ = registry.Models().Register(sizedClient{})
	_ = registry.Admin().Register(sizedClient{}, admin.Options{})
	_, store := migratedAdminDB(t, registry)
	_ = admin.CreateAccount(context.Background(), store, "admin", "secret")
	_ = registry.Register(admin.New(store))
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, _ := registry.Routes().Handler()
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")

	form := performAdminRequest(handler, http.MethodGet, "/admin/sized_client/new/", cookie).Body.String()

	if strings.Contains(form, "multipart/form-data") {
		t.Fatalf("a form with no file field is multipart: %s", form)
	}
}

func TestAdminCheckReportsFileFieldsOnTheLocalFallback(t *testing.T) {
	t.Setenv("TANGO_STORAGE_DIR", filepath.Join(t.TempDir(), "uploads"))
	registry := tango.NewRegistry()
	_ = registry.Models().Register(fileClient{})
	_ = registry.Admin().Register(fileClient{}, admin.Options{Widgets: map[string]admin.Widget{"Logo": admin.File(admin.FileOptions{Put: storage.PutOptions{MaxSize: 1 << 20}})}})
	_, store := migratedAdminDB(t, registry)
	_ = registry.Register(admin.New(store))
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}

	var failure error
	for _, check := range registry.Checks() {
		if check.Err != nil {
			failure = check.Err
		}
	}

	if failure == nil || !strings.Contains(failure.Error(), "fileClient.Logo") || !strings.Contains(failure.Error(), "SetStorage") {
		t.Fatalf("check failure = %v, want the field named and SetStorage suggested", failure)
	}
}

func TestAdminCheckIsQuietWhenEveryFileFieldHasAStore(t *testing.T) {
	registry := tango.NewRegistry()
	_ = registry.SetStorage(storagetest.NewMemory())
	_ = registry.Models().Register(fileClient{})
	_ = registry.Admin().Register(fileClient{}, admin.Options{Widgets: map[string]admin.Widget{"Logo": admin.File(admin.FileOptions{Put: storage.PutOptions{MaxSize: 1 << 20}})}})
	_, store := migratedAdminDB(t, registry)
	_ = registry.Register(admin.New(store))
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}

	for _, check := range registry.Checks() {
		if check.Err != nil {
			t.Fatalf("check %q failed: %v", check.Description, check.Err)
		}
	}
}

func TestAdminFileFieldAFailedSaveDoesNotShowTheDiscardedFileAsCurrent(t *testing.T) {
	store := newRecordingStore()
	handler, db, cookie := fileAdmin(t, logoField(store))
	_, _ = createWithLogo(t, handler, db, cookie, "first")
	path, old := createWithLogo(t, handler, db, cookie, "second")

	response := submitFiles(t, handler, cookie, path, [][2]string{{"Name", "Acme"}, {"Code", "first"}}, upload{"Logo", "new.png", pngBytes})

	body := response.Body.String()
	if !strings.Contains(body, old) || strings.Count(body, "https://cdn.test/") != 1 {
		t.Fatalf("the 422 form must show only the old file %q as current: %s", old, body)
	}
}

func TestAdminFileFieldReadOnlyTakesNoUploadAndStillSaves(t *testing.T) {
	store := newRecordingStore()
	registry := tango.NewRegistry()
	_ = registry.Models().Register(fileClient{})
	_ = registry.Admin().Register(fileClient{}, admin.Options{
		Widgets:  map[string]admin.Widget{"Logo": logoField(store)},
		ReadOnly: []string{"Logo"},
	})
	sqlDB, st := migratedAdminDB(t, registry)
	_ = admin.CreateAccount(context.Background(), st, "admin", "secret")
	_ = registry.Register(admin.New(st))
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, _ := registry.Routes().Handler()
	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
	if _, err := sqlDB.Exec(`INSERT INTO file_client (name, code, logo) VALUES ('Acme', 'acme', 'kept-key')`); err != nil {
		t.Fatal(err)
	}
	var id int64
	_ = sqlDB.QueryRow(`SELECT id FROM file_client`).Scan(&id)
	path := fmt.Sprintf("/admin/file_client/%d/", id)

	form := performAdminRequest(handler, http.MethodGet, path, cookie).Body.String()
	response := submitForm(t, handler, cookie, path, "Name", "Acme 2", "Code", "acme")

	if strings.Contains(form, "multipart/form-data") || response.Code != http.StatusFound || rowLogo(t, sqlDB, "acme") != "kept-key" || len(store.deleted) != 0 {
		t.Fatalf("status = %d, key = %q, deleted = %v, want a plain form that saves and keeps the key", response.Code, rowLogo(t, sqlDB, "acme"), store.deleted)
	}
}

// submitForm posts an urlencoded form with the page's CSRF token.
func submitForm(t *testing.T, handler http.Handler, cookie *http.Cookie, path string, pairs ...string) *httptest.ResponseRecorder {
	t.Helper()
	match := csrfInput.FindStringSubmatch(performAdminRequest(handler, http.MethodGet, path, cookie).Body.String())
	if match == nil {
		t.Fatal("no csrf token")
	}
	values := url.Values{"csrf_token": {match[1]}}
	for i := 0; i+1 < len(pairs); i += 2 {
		values.Set(pairs[i], pairs[i+1])
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storagetest"
	"github.com/angvp/tango/testdb"

	"uploads/migrations"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00"), bytes.Repeat([]byte("0123456789"), 40)...)

// spy remembers which keys were deleted from the object store.
type spy struct {
	storage.Store
	mu      sync.Mutex
	deleted []string
}

func (s *spy) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, key)
	s.mu.Unlock()
	return s.Store.Delete(ctx, key)
}

// site is the real application on a fresh, migrated database, with an
// in-memory object store.
type site struct {
	server   *httptest.Server
	store    *db.Store
	sqlDB    *sql.DB
	registry *tango.Registry
	objects  *spy
}

func newSite(t *testing.T) *site {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(sqlDB, dialect)
	objects := &spy{Store: storagetest.NewMemory()}
	registry, err := tango.BuildRegistry(appConfig(store, objects))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	registry.SetStore(store)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &site{server: server, store: store, sqlDB: sqlDB, registry: registry, objects: objects}
}

// client is one person's browser: it keeps the session cookie.
type client struct {
	t    *testing.T
	site *site
	http *http.Client
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// signIn creates an account and logs in through the real login form.
func (s *site) signIn(t *testing.T, email string) *client {
	t.Helper()
	hash, err := auth.HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := s.registry.Models().Get("Account")
	if err := s.store.Create(t.Context(), meta, &accounts.Account{Email: email, PasswordHash: hash, Active: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, site: s, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	page, err := c.http.Get(s.server.URL + "/accounts/login/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	token := csrfField.FindSubmatch(body)
	if token == nil {
		t.Fatalf("no csrf token on the login page: %s", body)
	}
	resp, err := c.http.PostForm(s.server.URL+"/accounts/login/", url.Values{"csrf_token": {string(token[1])}, "email": {email}, "password": {"correct-horse"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	return c
}

func (c *client) do(method, path string, body io.Reader, header http.Header) *http.Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.site.server.URL+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp
}

func read(resp *http.Response) string {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// upload posts one file as multipart form data.
func (c *client) upload(filename string, content []byte) *http.Response {
	c.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="` + filename + `"`},
		"Content-Type":        {"image/png"}, // declared by the client, never trusted
	})
	part.Write(content)
	w.Close()
	return c.do("POST", "/documents/", &buf, http.Header{"Content-Type": {w.FormDataContentType()}})
}

func (c *client) uploadOK(filename string, content []byte) int64 {
	c.t.Helper()
	resp := c.upload(filename, content)
	body := read(resp)
	if resp.StatusCode != http.StatusCreated {
		c.t.Fatalf("upload status = %d: %s", resp.StatusCode, body)
	}
	m := regexp.MustCompile(`"id":(\d+)`).FindStringSubmatch(body)
	id, _ := strconv.ParseInt(m[1], 10, 64)
	return id
}

func TestAppPassesChecks(t *testing.T) {
	s := newSite(t)
	registry, err := tango.BuildRegistry(appConfig(s.store, s.objects))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
}

func TestAnUploadIsStoredListedAndDownloadedByItsOwner(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	id := ada.uploadOK("../../etc/Ada's photo.png", pngBytes)

	list := read(ada.do("GET", "/documents/", nil, nil))
	if !strings.Contains(list, `"name":"Ada's photo.png"`) || !strings.Contains(list, `"content_type":"image/png"`) || strings.Contains(list, `"key"`) {
		t.Fatalf("list = %s; want the display name, the sniffed type and no key", list)
	}

	resp := ada.do("GET", "/documents/"+strconv.FormatInt(id, 10)+"/download/", nil, nil)
	disposition := resp.Header.Get("Content-Disposition")
	got := read(resp)
	if resp.StatusCode != http.StatusOK || got != string(pngBytes) || !strings.HasPrefix(disposition, "attachment") ||
		resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: status %d, disposition %q, %d bytes", resp.StatusCode, disposition, len(got))
	}
}

func TestADownloadCanBeInlineForAnImageAndSupportsRanges(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	path := "/documents/" + strconv.FormatInt(ada.uploadOK("pic.png", pngBytes), 10) + "/download/"

	inline := ada.do("GET", path+"?inline=1", nil, nil)
	inline.Body.Close()
	if !strings.HasPrefix(inline.Header.Get("Content-Disposition"), "inline") {
		t.Errorf("Content-Disposition = %q, want inline for an image when asked", inline.Header.Get("Content-Disposition"))
	}
	ranged := ada.do("GET", path, nil, http.Header{"Range": {"bytes=0-9"}})
	if body := read(ranged); ranged.StatusCode != http.StatusPartialContent || body != string(pngBytes[:10]) {
		t.Errorf("range: status %d, body %q", ranged.StatusCode, body)
	}
}

func TestAnotherAccountAndAnonymousVisitorsCannotReachADocument(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	grace := s.signIn(t, "grace@example.com")
	id := strconv.FormatInt(ada.uploadOK("ada.png", pngBytes), 10)

	for _, tt := range []struct {
		name         string
		who          *client
		method, path string
		want         int
	}{
		{"download", grace, "GET", "/documents/" + id + "/download/", http.StatusNotFound},
		{"delete", grace, "DELETE", "/documents/" + id + "/", http.StatusNotFound},
		{"a missing id", grace, "GET", "/documents/999/download/", http.StatusNotFound},
	} {
		if got := tt.who.do(tt.method, tt.path, nil, nil).StatusCode; got != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, got, tt.want)
		}
	}
	if list := read(grace.do("GET", "/documents/", nil, nil)); strings.Contains(list, "ada.png") {
		t.Errorf("grace sees ada's document: %s", list)
	}

	anonymous := &client{t: t, site: s, http: &http.Client{}}
	for _, path := range []string{"/documents/", "/documents/" + id + "/download/"} {
		if got := anonymous.do("GET", path, nil, nil).StatusCode; got != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d, want 401", path, got)
		}
	}
	if got := anonymous.upload("x.png", pngBytes).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("anonymous upload = %d, want 401", got)
	}
}

func TestUploadsAreHeldToTheHostsLimits(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")

	tooBig := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte("x"), 4<<20)...)
	if resp := ada.upload("big.png", tooBig); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("over the file cap: status %d, want 413", resp.StatusCode)
	}
	if resp := ada.upload("page.png", []byte("<html><script>alert(1)</script></html>")); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("html declared as png: status %d, want 415", resp.StatusCode)
	}
	if resp := ada.do("POST", "/documents/", strings.NewReader(`{"not":"multipart"}`), http.Header{"Content-Type": {"application/json"}}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a JSON body: status %d, want 400", resp.StatusCode)
	}
	// The routes that carry no file keep their small body limit.
	if resp := ada.do("DELETE", "/documents/1/", strings.NewReader(strings.Repeat("a", 70<<10)), nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("a large body on a JSON route: status %d, want 413", resp.StatusCode)
	}
	if list := read(ada.do("GET", "/documents/", nil, nil)); list != "[]\n" && list != "[]" {
		t.Errorf("refused uploads left rows behind: %s", list)
	}
}

func TestDeletingARowThenItsBytes(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	id := strconv.FormatInt(ada.uploadOK("ada.png", pngBytes), 10)

	if got := ada.do("DELETE", "/documents/"+id+"/", nil, nil).StatusCode; got != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", got)
	}
	if got := ada.do("GET", "/documents/"+id+"/download/", nil, nil).StatusCode; got != http.StatusNotFound {
		t.Errorf("download after delete = %d, want 404", got)
	}
	if len(s.objects.deleted) != 1 {
		t.Errorf("object deletions = %v, want the document's one object", s.objects.deleted)
	}
}

func TestAFailedRowWriteDeletesTheNewObject(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	if _, err := s.sqlDB.ExecContext(t.Context(), `DROP TABLE document`); err != nil {
		t.Fatal(err)
	}
	if resp := ada.upload("ada.png", pngBytes); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("upload with no table: status %d, want 500", resp.StatusCode)
	}
	if len(s.objects.deleted) != 1 {
		t.Fatalf("deleted objects = %v, want the one just stored", s.objects.deleted)
	}
	if _, err := s.objects.Stat(t.Context(), s.objects.deleted[0]); err == nil {
		t.Error("the object written before the failed row write is still stored")
	}
}

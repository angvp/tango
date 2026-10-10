package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"

	"uploads/apps/documents"
)

const staffPassword = "correct-horse"

// staffSignIn creates a staff admin account and logs in through the real
// admin login form. The client keeps only the admin session cookie, so it
// is not an accounts session.
func (s *site) staffSignIn(t *testing.T, username string) *client {
	t.Helper()
	if err := admin.CreateAccount(context.Background(), s.store, username, staffPassword); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, site: s, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	page := c.do("GET", "/admin/login/", nil, nil)
	body := read(page)
	token := csrfField.FindSubmatch([]byte(body))
	if token == nil {
		t.Fatalf("no csrf token on the admin login page: %s", body)
	}
	resp := c.do("POST", "/admin/login/", strings.NewReader(url.Values{
		"csrf_token": {string(token[1])},
		"username":   {username},
		"password":   {staffPassword},
	}.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	read(resp)
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusFound {
		t.Fatalf("admin login status = %d", resp.StatusCode)
	}
	return c
}

// sha256Hex is the digest storage records for a file's bytes.
func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// downloadLink is the href the changelist renders for the Download column.
var downloadLink = regexp.MustCompile(`<a href="([^"]+)" rel="noopener noreferrer">Download</a>`)

func TestAdminChangelistShowsSizeHashAndAnAuthorizedDownloadLink(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	staff := s.staffSignIn(t, "staff")
	ada.uploadOK("photo.png", pngBytes)

	resp := staff.do("GET", "/admin/document/", nil, nil)
	list := read(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changelist status = %d: %s", resp.StatusCode, list)
	}
	for _, want := range []string{"photo.png", admin.ByteSize(int64(len(pngBytes))), admin.ShortHash(sha256Hex(pngBytes))} {
		if !strings.Contains(list, want) {
			t.Errorf("changelist is missing %q", want)
		}
	}
	if link := downloadLink.FindString(list); link == "" {
		t.Errorf("changelist has no download link")
	}
	if key := s.storageKey(t, 1); strings.Contains(list, key) {
		t.Errorf("changelist exposes the storage key %q", key)
	}
}

// The link the changelist renders is followed as-is: the owner gets the
// bytes, and an admin session (no accounts session) is refused.
func TestAdminDownloadLinkIsTheRenderedAuthorizedRoute(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	staff := s.staffSignIn(t, "staff")
	ada.uploadOK("photo.png", pngBytes)

	m := downloadLink.FindStringSubmatch(read(staff.do("GET", "/admin/document/", nil, nil)))
	if m == nil {
		t.Fatal("no download link in the changelist")
	}
	link := m[1]

	owner := ada.do("GET", link, nil, nil)
	if got := read(owner); owner.StatusCode != http.StatusOK || got != string(pngBytes) {
		t.Fatalf("owner follows the rendered link: status %d, %d bytes", owner.StatusCode, len(got))
	}
	if got := staff.do("GET", link, nil, nil).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("staff admin follows the rendered link: status %d, want 401", got)
	}
}

func TestAdminChangelistRefusesAnonymousVisitors(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	ada.uploadOK("secret-plans.png", pngBytes)

	anonymous := &client{t: t, site: s, http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	resp := anonymous.do("GET", "/admin/document/", nil, nil)
	if body := read(resp); strings.Contains(body, "secret-plans.png") {
		t.Fatalf("anonymous visitor sees a file name: status %d", resp.StatusCode)
	}
}

// adminForm loads an admin page and returns its CSRF token, for a POST.
func adminForm(t *testing.T, c *client, path string) (string, string) {
	t.Helper()
	body := read(c.do("GET", path, nil, nil))
	token := csrfField.FindStringSubmatch(body)
	if token == nil {
		t.Fatalf("no csrf token on %s", path)
	}
	return token[1], body
}

func TestAdminEditFormNeverShowsTheStorageKey(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	staff := s.staffSignIn(t, "staff")
	id := ada.uploadOK("photo.png", pngBytes)

	_, body := adminForm(t, staff, "/admin/document/"+strconv.FormatInt(id, 10)+"/")
	if !strings.Contains(body, "photo.png") {
		t.Fatalf("edit form does not show the file name:\n%s", body)
	}
	if key := s.storageKey(t, id); strings.Contains(body, key) {
		t.Fatalf("edit form exposes the storage key %q", key)
	}
}

func TestAdminEditCannotRepointTheStoredObjectOrChangeOwnership(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	grace := s.signIn(t, "grace@example.com")
	staff := s.staffSignIn(t, "staff")
	adaID := ada.uploadOK("ada.png", pngBytes)
	graceID := grace.uploadOK("grace.png", append(append([]byte{}, pngBytes...), 'g'))
	adaKey, graceKey := s.storageKey(t, adaID), s.storageKey(t, graceID)
	before := s.document(t, adaID)

	path := "/admin/document/" + strconv.FormatInt(adaID, 10) + "/"
	token, _ := adminForm(t, staff, path)
	resp := staff.do("POST", path, strings.NewReader(url.Values{
		"csrf_token": {token}, "Name": {"renamed.png"}, "Key": {graceKey},
		"OwnerID": {strconv.FormatInt(before.OwnerID+1, 10)}, "Size": {"1"}, "SHA256": {"forged"},
	}.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	read(resp)

	after := s.document(t, adaID)
	if after.Key != adaKey || after.Key == graceKey {
		t.Fatalf("edit repointed the object: key %q, want %q", after.Key, adaKey)
	}
	if after.OwnerID != before.OwnerID || after.Size != before.Size || after.SHA256 != before.SHA256 {
		t.Fatalf("edit changed host metadata: %+v, was %+v", after, before)
	}
	if after.Name != "renamed.png" {
		t.Fatalf("the editable name did not save: %q", after.Name)
	}
}

func TestAdminCannotCreateADocumentWithoutAnObject(t *testing.T) {
	s := newSite(t)
	staff := s.staffSignIn(t, "staff")
	token, _ := adminForm(t, staff, "/admin/document/new/")

	resp := staff.do("POST", "/admin/document/new/", strings.NewReader(url.Values{
		"csrf_token": {token}, "Name": {"ghost.png"}, "OwnerID": {"1"}, "Size": {"9"},
	}.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	read(resp)

	var count int
	if err := s.sqlDB.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || resp.StatusCode == http.StatusSeeOther {
		t.Fatalf("admin created a document row without an object: status %d, rows %d", resp.StatusCode, count)
	}
}

// document reads the row for id through the store, so the test sees what
// the database holds.
func (s *site) document(t *testing.T, id int64) documents.Document {
	t.Helper()
	meta, _ := s.registry.Models().Get("Document")
	var doc documents.Document
	if err := s.store.Get(t.Context(), meta, id, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// storageKey returns the object key the host stored for document id, so the
// test can prove the admin never shows it.
func (s *site) storageKey(t *testing.T, id int64) string {
	t.Helper()
	return s.document(t, id).Key
}

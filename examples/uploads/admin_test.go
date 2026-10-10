package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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

func TestAdminChangelistShowsSizeHashAndAnAuthorizedDownloadLink(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	staff := s.staffSignIn(t, "staff")
	id := ada.uploadOK("photo.png", pngBytes)

	resp := staff.do("GET", "/admin/document/", nil, nil)
	list := read(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changelist status = %d: %s", resp.StatusCode, list)
	}

	wantSize := admin.ByteSize(int64(len(pngBytes)))
	wantHash := admin.ShortHash(sha256Hex(pngBytes))
	wantLink := `<a href="/documents/` + strconv.FormatInt(id, 10) + `/download/" rel="noopener noreferrer">Download</a>`
	for _, want := range []string{"photo.png", wantSize, wantHash, wantLink} {
		if !strings.Contains(list, want) {
			t.Errorf("changelist is missing %q", want)
		}
	}
	if key := s.storageKey(t, id); strings.Contains(list, key) {
		t.Errorf("changelist exposes the storage key %q", key)
	}
}

func TestAdminDownloadLinkIsTheOwnersAuthorizedRouteAndNoBypass(t *testing.T) {
	s := newSite(t)
	ada := s.signIn(t, "ada@example.com")
	staff := s.staffSignIn(t, "staff")
	id := ada.uploadOK("photo.png", pngBytes)
	link := "/documents/" + strconv.FormatInt(id, 10) + "/download/"

	owner := ada.do("GET", link, nil, nil)
	if got := read(owner); owner.StatusCode != http.StatusOK || got != string(pngBytes) {
		t.Fatalf("owner follows the link: status %d, %d bytes", owner.StatusCode, len(got))
	}

	// The admin session does not authorize a download: the link still asks
	// for the owner's accounts session, and a staff account is not one.
	if got := staff.do("GET", link, nil, nil).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("staff admin follows the link: status %d, want 401", got)
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

// storageKey returns the object key the host stored for document id, so the
// test can prove the admin never shows it.
func (s *site) storageKey(t *testing.T, id int64) string {
	t.Helper()
	meta, _ := s.registry.Models().Get("Document")
	var doc documents.Document
	if err := s.store.Get(t.Context(), meta, id, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Key
}

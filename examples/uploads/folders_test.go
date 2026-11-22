package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// A staff user sees the Storage page, and uploads a folder's cover from the
// admin form: the object lands in the host's store and the row keeps its key.
func TestStaffUploadsAFolderCoverFromTheAdmin(t *testing.T) {
	s := newSite(t)
	staff := s.staffSignIn(t, "root")

	page := read(staff.do("GET", "/admin/storage/", nil, nil))
	if !strings.Contains(page, "the site holds 0 documents") {
		t.Fatalf("the Storage page does not report the document count: %s", page)
	}

	form := read(staff.do("GET", "/admin/folder/new/", nil, nil))
	token := csrfField.FindStringSubmatch(form)
	if token == nil || !strings.Contains(form, `enctype="multipart/form-data"`) {
		t.Fatalf("the folder form is not a multipart form with a token: %s", form)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("csrf_token", token[1])
	_ = writer.WriteField("Name", "Holidays")
	part, _ := writer.CreateFormFile("Cover", "cover.png")
	_, _ = part.Write(pngBytes)
	_ = writer.Close()
	resp := staff.do("POST", "/admin/folder/new/", &body, http.Header{"Content-Type": {writer.FormDataContentType()}})
	read(resp)
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d, want a redirect", resp.StatusCode)
	}

	var key string
	if err := s.sqlDB.QueryRow(`SELECT cover FROM folder WHERE name = 'Holidays'`).Scan(&key); err != nil || key == "" {
		t.Fatalf("the folder row holds cover %q (err %v), want the generated key", key, err)
	}
	if _, err := s.objects.Stat(t.Context(), key); err != nil {
		t.Fatalf("the cover object is not in the host's store: %v", err)
	}
}

// An anonymous visitor gets no part of the admin page.
func TestTheStoragePageIsForStaffOnly(t *testing.T) {
	s := newSite(t)
	jar := (&client{t: t, site: s, http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})

	resp := jar.do("GET", "/admin/storage/", nil, nil)
	read(resp)

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous status = %d, want a redirect to the login", resp.StatusCode)
	}
}

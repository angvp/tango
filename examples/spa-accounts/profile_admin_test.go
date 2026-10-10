package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
)

const adminSessionToken = "spa-admin-session"

// adminSite registers a staff admin account with a live session on s, and
// returns a request helper that sends the admin session cookie and the
// CSRF token admin derives from it.
func adminSite(t *testing.T, s *site) func(method, path string, form url.Values) response {
	t.Helper()
	store := db.NewStore(s.db, s.dialect)
	if err := admin.CreateAccount(context.Background(), store, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO admin_session (token, user_id, expires_at) SELECT $1, id, $2 FROM admin_user WHERE username = $3`,
		adminSessionToken, time.Now().Add(time.Hour).UTC(), "admin",
	); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(adminSessionToken))
	csrf := hex.EncodeToString(sum[:])
	return func(method, path string, form url.Values) response {
		t.Helper()
		values := url.Values{}
		for k, v := range form {
			values[k] = v
		}
		values.Set("csrf_token", csrf)
		req, _ := http.NewRequest(method, path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: adminSessionToken})
		recorder := httptest.NewRecorder()
		s.handler.ServeHTTP(recorder, req)
		return response{recorder}
	}
}

// profileID registers an account whose profile has the username, and
// returns the account and profile ids.
func profileID(t *testing.T, s *site, username string) (accountID, profileID int64) {
	t.Helper()
	r := s.post("/accounts/api/register/", `{"email":"`+username+`@example.com","password":"correct-horse","profile":{"username":"`+username+`"}}`)
	if r.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", r.Code, r.Body.String())
	}
	if err := s.db.QueryRow(`SELECT account_id, id FROM profile WHERE username = $1`, username).Scan(&accountID, &profileID); err != nil {
		t.Fatal(err)
	}
	return accountID, profileID
}

var profileTextarea = regexp.MustCompile(`(?s)<textarea id="field-Preferences" name="Preferences" class="input json-input"[^>]*>\n(.*?)</textarea>`)

func TestProfileAdminEditsPreferencesAsJSON(t *testing.T) {
	s := newSite(t)
	request := adminSite(t, s)
	accountID, id := profileID(t, s, "ada")
	path := "/admin/profile/" + strconv.FormatInt(id, 10) + "/"
	form := url.Values{"AccountID": {strconv.FormatInt(accountID, 10)}, "Username": {"ada"}, "DisplayName": {"ada"}, "Bio": {""}}

	if r := request(http.MethodGet, path, nil); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `data-tango-widget="json"`) {
		t.Fatalf("edit page: %d\n%s", r.Code, r.Body.String())
	}

	invalid := "{\"theme\": \"dark\",\n"
	bad := request(http.MethodPost, path, merge(form, url.Values{"Preferences": {invalid}}))
	if bad.Code != http.StatusUnprocessableEntity || !strings.Contains(bad.Body.String(), "Invalid JSON") {
		t.Fatalf("invalid preferences: %d\n%s", bad.Code, bad.Body.String())
	}
	if m := profileTextarea.FindStringSubmatch(bad.Body.String()); m == nil || htmlUnescape(m[1]) != invalid {
		t.Fatalf("submitted text not preserved: %v", m)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT preferences FROM profile WHERE id = $1`, id).Scan(&stored); err != nil || stored != "" {
		t.Fatalf("a refused save changed preferences to %q (%v)", stored, err)
	}

	saved := "{\n  \"theme\" : \"dark\",\n  \"digest\": true\n}"
	ok := request(http.MethodPost, path, merge(form, url.Values{"Preferences": {saved}}))
	if ok.Code != http.StatusFound {
		t.Fatalf("valid preferences: %d\n%s", ok.Code, ok.Body.String())
	}
	if err := s.db.QueryRow(`SELECT preferences FROM profile WHERE id = $1`, id).Scan(&stored); err != nil || stored != saved {
		t.Fatalf("stored %q (%v), want exactly %q", stored, err, saved)
	}
}

func merge(a, b url.Values) url.Values {
	out := url.Values{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/internal/security"
)

// flashCookieName is the cookie an outcome travels in across a redirect.
const flashCookieName = "tango_admin_flash"

// secondSessionToken is a second, active admin session on the same account.
const secondSessionToken = "second-session-token"

// flashCookieMaxAgeForTest mirrors the server's flash lifetime; the expiry
// test seals a value just outside it.
const flashCookieMaxAgeForTest = 300 * time.Second

var statusRegionPattern = regexp.MustCompile(`role="status"`)

// signedRequest sends one request as the session token holds. A form is
// submitted with the CSRF token that session renders; extra cookies travel
// with the request.
func signedRequest(handler http.Handler, method, path, token string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var body io.Reader = strings.NewReader("")
	if form != nil {
		submitted := url.Values{}
		for key, values := range form {
			submitted[key] = values
		}
		submitted.Set("csrf_token", security.DeriveCSRFToken(token))
		body = strings.NewReader(submitted.Encode())
	}
	request := httptest.NewRequest(method, path, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: token})
	}
	for _, cookie := range cookies {
		if cookie != nil {
			request.AddCookie(cookie)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// flashSetBy returns the flash cookie a response sets, or nil when it sets
// none with a value.
func flashSetBy(response *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == flashCookieName && cookie.Value != "" && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	return nil
}

// mustFlash returns the flash cookie a response sets and fails the test when
// there is none, so a "not shown" assertion always has a shown outcome beside it.
func mustFlash(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookie := flashSetBy(response)
	if cookie == nil {
		t.Fatalf("response set no flash cookie: %d", response.Code)
	}
	return cookie
}

// flashClearedBy reports whether a response expires the flash cookie.
func flashClearedBy(response *httptest.ResponseRecorder) bool {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == flashCookieName && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func addSecondSession(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO admin_session (token, user_id, expires_at) SELECT $1, id, $2 FROM admin_user WHERE username = $3`,
		secondSessionToken, time.Now().Add(time.Hour).UTC(), "admin",
	); err != nil {
		t.Fatalf("seed second session: %v", err)
	}
}

func TestBulkDeleteOutcomeIsAnnouncedOnceAndTheRedirectCarriesNoQuery(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)

	posted := signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second))
	if posted.Code != http.StatusFound {
		t.Fatalf("bulk delete status = %d, want %d", posted.Code, http.StatusFound)
	}
	if location := posted.Header().Get("Location"); location != crudBasePath {
		t.Fatalf("bulk delete redirect = %q, want %q", location, crudBasePath)
	}
	flash := flashSetBy(posted)
	if flash == nil {
		t.Fatal("bulk delete set no flash cookie")
	}
	if !flash.HttpOnly || flash.SameSite != http.SameSiteLaxMode || flash.Path != "/admin/" {
		t.Fatalf("flash cookie attributes = HttpOnly %v, SameSite %v, Path %q", flash.HttpOnly, flash.SameSite, flash.Path)
	}

	list := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash)
	if !strings.Contains(list.Body.String(), "2 crud products deleted") {
		t.Fatalf("list after bulk delete lacks the outcome: %s", list.Body.String())
	}
	if n := len(statusRegionPattern.FindAllString(list.Body.String(), -1)); n != 1 {
		t.Fatalf("status regions = %d, want 1", n)
	}
	if !flashClearedBy(list) {
		t.Fatal("the render did not expire the flash cookie")
	}

	// The next page is requested without the cookie, as a browser sends it
	// once the render has expired it.
	again := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil)
	if strings.Contains(again.Body.String(), "crud products deleted") {
		t.Fatal("the outcome was shown again on the next page")
	}
}

func TestBulkDeleteOfOneRowUsesTheSingularOutcome(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	only := seedProduct(t, sqlDB, "A", 1)

	posted := signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(1, only))
	list := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flashSetBy(posted))
	if !strings.Contains(list.Body.String(), "1 crud product deleted") {
		t.Fatalf("list lacks the singular outcome: %s", list.Body.String())
	}
}

func TestCreateEditAndDeleteAnnounceTheirOutcome(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())

	created := signedRequest(handler, http.MethodPost, crudBasePath+"new/", testSessionToken, url.Values{"Name": {"New"}, "Price": {"3"}})
	if body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flashSetBy(created)).Body.String(); !strings.Contains(body, "The crud product was added.") {
		t.Fatalf("create outcome missing: %s", body)
	}

	row := seedProduct(t, sqlDB, "Edit me", 2)
	saved := signedRequest(handler, http.MethodPost, crudBasePath+strconv.FormatInt(row, 10)+"/", testSessionToken, url.Values{"Name": {"Edited"}, "Price": {"2"}})
	if body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flashSetBy(saved)).Body.String(); !strings.Contains(body, "The crud product was saved.") {
		t.Fatalf("save outcome missing: %s", body)
	}

	removed := signedRequest(handler, http.MethodPost, crudBasePath+strconv.FormatInt(row, 10)+"/delete/", testSessionToken, url.Values{})
	if body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flashSetBy(removed)).Body.String(); !strings.Contains(body, "The crud product was deleted.") {
		t.Fatalf("delete outcome missing: %s", body)
	}
}

func TestEveryAdminPageRendersOneStatusRegion(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions(), admin.WithDashboard(admin.DashboardConfig{
		Status: func(context.Context) (admin.DashboardStatus, error) {
			return admin.DashboardStatus{Database: admin.DatabaseReachable, Dialect: "SQLite", TrackingTable: admin.TrackingTablePresent, Release: "test"}, nil
		},
	}))
	row := seedProduct(t, sqlDB, "Only", 1)

	pages := map[string]string{
		"login":     "",
		"list":      crudBasePath,
		"create":    crudBasePath + "new/",
		"edit":      crudBasePath + strconv.FormatInt(row, 10) + "/",
		"delete":    crudBasePath + strconv.FormatInt(row, 10) + "/delete/",
		"dashboard": "/admin/",
	}
	for name, path := range pages {
		token := testSessionToken
		if name == "login" {
			token = ""
		}
		body := signedRequest(handler, http.MethodGet, orLogin(path), token, nil).Body.String()
		if n := len(statusRegionPattern.FindAllString(body, -1)); n != 1 {
			t.Errorf("%s: status regions = %d, want 1", name, n)
		}
	}
}

func TestOutcomeFlashIsBoundToTheSessionThatSetIt(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	addSecondSession(t, sqlDB)
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))

	other := signedRequest(handler, http.MethodGet, crudBasePath, secondSessionToken, nil, flash)
	if strings.Contains(other.Body.String(), "crud products deleted") {
		t.Fatal("another session was shown the outcome")
	}
	owner := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash)
	if !strings.Contains(owner.Body.String(), "2 crud products deleted") {
		t.Fatal("the session that set the outcome was not shown it")
	}
}

func TestOutcomeFlashIsNotShownToALoggedOutRequest(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))

	login := signedRequest(handler, http.MethodGet, "/admin/login/", "", nil, flash)
	if strings.Contains(login.Body.String(), "crud products deleted") {
		t.Fatal("the login page showed an outcome to a logged-out request")
	}
	if n := len(statusRegionPattern.FindAllString(login.Body.String(), -1)); n != 1 {
		t.Fatalf("login status regions = %d, want 1", n)
	}
	list := signedRequest(handler, http.MethodGet, crudBasePath, "", nil, flash)
	if list.Code != http.StatusFound || strings.Contains(list.Body.String(), "crud products deleted") {
		t.Fatalf("logged-out list = %d, want a redirect with no outcome", list.Code)
	}
}

func TestOutcomeFlashStopsBeingAcceptedWhenItsSessionEnds(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))

	if live := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash); !strings.Contains(live.Body.String(), "crud products deleted") {
		t.Fatal("the live session was not shown the outcome; the ended-session check would prove nothing")
	}
	if out := signedRequest(handler, http.MethodPost, "/admin/logout/", testSessionToken, url.Values{}); out.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", out.Code, http.StatusFound)
	}
	ended := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash)
	if ended.Code != http.StatusFound || strings.Contains(ended.Body.String(), "crud products deleted") {
		t.Fatalf("ended session = %d, want a redirect with no outcome", ended.Code)
	}
}

func TestForgedOrAlteredOutcomeFlashIsIgnored(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	genuine := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second))).Value
	if body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, &http.Cookie{Name: flashCookieName, Value: genuine}).Body.String(); !strings.Contains(body, "2 crud products deleted") {
		t.Fatal("the genuine outcome was not shown; the forged values below would prove nothing")
	}

	payload, mac, _ := strings.Cut(genuine, ".")
	raise := base64.RawURLEncoding.EncodeToString([]byte(`{"k":"admin.bulk.outcome_many","a":["9","crud products"]}`))
	// The first character carries six significant bits of the MAC. The last
	// character can carry only padding bits, so flipping it can leave the
	// decoded MAC unchanged.
	flipped := "A" + mac[1:]
	if mac[0] == 'A' {
		flipped = "B" + mac[1:]
	}

	forged := map[string]string{
		"garbage":         "garbage",
		"no separator":    payload,
		"altered payload": raise + "." + mac,
		"altered mac":     payload + "." + flipped,
		"oversized":       strings.Repeat("a", 8192),
		"bad base64":      "!!!." + mac,
		"two separators":  genuine + ".x",
	}
	for name, value := range forged {
		response := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, &http.Cookie{Name: flashCookieName, Value: value})
		body := response.Body.String()
		for _, leak := range []string{"crud products deleted", "9 crud products", "admin.bulk.outcome"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: body shows %q", name, leak)
			}
		}
		if n := len(statusRegionPattern.FindAllString(body, -1)); n != 1 {
			t.Errorf("%s: status regions = %d, want 1", name, n)
		}
	}
}

func TestAnUnknownKeyWithAValidMACIsNeverRendered(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, bulkProductOptions())
	known, err := admin.SealFlashAtForTest(testSessionToken, "admin.outcome.deleted", time.Now(), "crud product")
	if err != nil {
		t.Fatalf("seal known: %v", err)
	}
	if body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, &http.Cookie{Name: flashCookieName, Value: known}).Body.String(); !strings.Contains(body, "The crud product was deleted.") {
		t.Fatal("a genuine known outcome was not shown; the rejections below would prove nothing")
	}
	for _, tc := range []struct {
		name string
		key  string
		args []string
	}{
		{"unknown key", "admin.never.catalogued", []string{"secret-argument"}},
		{"known key, wrong arity", "admin.bulk.outcome_many", []string{"secret-argument"}},
	} {
		value, err := admin.SealFlashAtForTest(testSessionToken, tc.key, time.Now(), tc.args...)
		if err != nil {
			t.Fatalf("%s: seal: %v", tc.name, err)
		}
		body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, &http.Cookie{Name: flashCookieName, Value: value}).Body.String()
		for _, leak := range []string{tc.key, "secret-argument"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: body shows %q", tc.name, leak)
			}
		}
	}
}

func TestDismissHidesTheOutcomeAndItDoesNotReturn(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))

	shown := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash).Body.String()
	if !strings.Contains(shown, "2 crud products deleted") || !strings.Contains(shown, "Dismiss") {
		t.Fatalf("outcome with its Dismiss control not shown: %s", shown)
	}
	if !strings.Contains(shown, `action="/admin/messages/dismiss/"`) {
		t.Fatal("the outcome has no dismiss form")
	}

	dismissed := signedRequest(handler, http.MethodPost, "/admin/messages/dismiss/", testSessionToken, url.Values{"next": {crudBasePath}})
	if dismissed.Code != http.StatusFound || dismissed.Header().Get("Location") != crudBasePath {
		t.Fatalf("dismiss = %d to %q, want a redirect to the list", dismissed.Code, dismissed.Header().Get("Location"))
	}
	after := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil).Body.String()
	if strings.Contains(after, "crud products deleted") || strings.Contains(after, "messages/dismiss") {
		t.Fatal("the dismissed outcome came back on the next page")
	}
}

func TestDismissRejectsAMissingCSRFToken(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, bulkProductOptions())
	request := httptest.NewRequest(http.MethodPost, "/admin/messages/dismiss/", strings.NewReader("next=/admin/crud_product/"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("dismiss without a CSRF token = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAPostLeavesTheOutcomeForTheNextPage(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))

	rejected := signedRequest(handler, http.MethodPost, crudBasePath+"new/", testSessionToken,
		url.Values{"Name": {"Broken"}, "Price": {"not-a-number"}}, flash)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rejected save = %d, want %d", rejected.Code, http.StatusUnprocessableEntity)
	}
	if flashClearedBy(rejected) {
		t.Fatal("a POST expired the outcome before the next page could show it")
	}
}

func orLogin(path string) string {
	if path == "" {
		return "/admin/login/"
	}
	return path
}

func TestAnOutcomeOutsideItsLifetimeIsIgnored(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, bulkProductOptions())
	fresh, err := admin.SealFlashAtForTest(testSessionToken, "admin.outcome.deleted", time.Now(), "crud product")
	if err != nil {
		t.Fatalf("seal fresh: %v", err)
	}
	if body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, &http.Cookie{Name: flashCookieName, Value: fresh}).Body.String(); !strings.Contains(body, "The crud product was deleted.") {
		t.Fatal("a fresh outcome was not shown; the rejections below would prove nothing")
	}
	for name, issued := range map[string]time.Time{
		"older than the lifetime": time.Now().Add(-(flashCookieMaxAgeForTest + time.Minute)),
		"issued in the future":    time.Now().Add(10 * time.Minute),
	} {
		stale, err := admin.SealFlashAtForTest(testSessionToken, "admin.outcome.deleted", issued, "crud product")
		if err != nil {
			t.Fatalf("%s: seal: %v", name, err)
		}
		body := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, &http.Cookie{Name: flashCookieName, Value: stale}).Body.String()
		if strings.Contains(body, "The crud product was deleted.") {
			t.Errorf("%s: the outcome was shown", name)
		}
	}
}

func TestAnErroringPageLeavesTheOutcomeForTheNextPage(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	flash := mustFlash(t, signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, first, second)))

	missing := signedRequest(handler, http.MethodGet, crudBasePath+"999999/", testSessionToken, nil, flash)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing row = %d, want %d", missing.Code, http.StatusNotFound)
	}
	if flashClearedBy(missing) {
		t.Fatal("a 404 expired the outcome before any page showed it")
	}

	next := signedRequest(handler, http.MethodGet, crudBasePath, testSessionToken, nil, flash)
	if !strings.Contains(next.Body.String(), "2 crud products deleted") {
		t.Fatal("the next page did not show the outcome the 404 left in place")
	}
}

func TestAnOutcomeThatCannotBeCarriedStillRedirectsAndIsLogged(t *testing.T) {
	var logs bytes.Buffer
	restore := captureDefaultLog(&logs)
	defer restore()

	options := bulkProductOptions()
	options.SingularName = strings.Repeat("very long noun ", 200)
	handler, sqlDB := buildProductAdminWithOptions(t, options)
	row := seedProduct(t, sqlDB, "Edit me", 2)
	other := seedProduct(t, sqlDB, "Bulk me", 3)
	another := seedProduct(t, sqlDB, "Bulk me too", 4)

	cases := []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{"create", http.MethodPost, crudBasePath + "new/", url.Values{"Name": {"New"}, "Price": {"3"}}},
		{"save", http.MethodPost, crudBasePath + strconv.FormatInt(row, 10) + "/", url.Values{"Name": {"Edited"}, "Price": {"2"}}},
		{"delete", http.MethodPost, crudBasePath + strconv.FormatInt(other, 10) + "/delete/", url.Values{}},
		{"bulk delete", http.MethodPost, bulkDeletePath, bulkForm(1, another)},
	}
	for _, tc := range cases {
		response := signedRequest(handler, tc.method, tc.path, testSessionToken, tc.form)
		if response.Code != http.StatusFound {
			t.Errorf("%s: status = %d, want a redirect after the committed write", tc.name, response.Code)
		}
		if flashSetBy(response) != nil {
			t.Errorf("%s: an oversized outcome was set", tc.name)
		}
	}
	if !strings.Contains(logs.String(), "admin outcome not carried") {
		t.Fatalf("the dropped outcome was not logged: %s", logs.String())
	}
	if countProducts(t, sqlDB) != 2 {
		t.Fatalf("product count = %d, want 2 (create, then the delete and bulk delete remove two of three)", countProducts(t, sqlDB))
	}
}

func TestRejectedPagesHaveOneStatusRegion(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	row := seedProduct(t, sqlDB, "Only", 1)

	bulkRejected := signedRequest(handler, http.MethodPost, bulkDeletePath, testSessionToken, bulkForm(2, row))
	if bulkRejected.Code != http.StatusBadRequest {
		t.Fatalf("bulk rejection = %d, want %d", bulkRejected.Code, http.StatusBadRequest)
	}
	if n := len(statusRegionPattern.FindAllString(bulkRejected.Body.String(), -1)); n != 1 {
		t.Errorf("bulk rejection: status regions = %d, want 1", n)
	}

	failedSave := signedRequest(handler, http.MethodPost, crudBasePath+"new/", testSessionToken, url.Values{"Name": {"Broken"}, "Price": {"not-a-number"}})
	if failedSave.Code != http.StatusUnprocessableEntity {
		t.Fatalf("failed save = %d, want %d", failedSave.Code, http.StatusUnprocessableEntity)
	}
	if n := len(statusRegionPattern.FindAllString(failedSave.Body.String(), -1)); n != 1 {
		t.Errorf("failed save: status regions = %d, want 1", n)
	}
}

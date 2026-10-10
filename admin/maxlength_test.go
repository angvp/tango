package admin_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
)

type adminHeadline struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=5"`
	Body  string
	Note  string `tango:"text"`
}

const headlineBasePath = "/admin/admin_headline/"

func buildHeadlineAdmin(t *testing.T, adminOpts ...admin.Option) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminHeadline{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminHeadline{}, admin.Options{}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store, adminOpts...)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB
}

func headlineCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_headline`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBoundedFieldRendersMaxlengthAndOthersAreUnchanged(t *testing.T) {
	handler, _ := buildHeadlineAdmin(t)
	body := doRequest(t, handler, http.MethodGet, headlineBasePath+"new/", nil).Body.String()

	if !strings.Contains(body, `<input id="field-Title" type="text" name="Title" value="" maxlength="5" class="input">`) {
		t.Errorf("bounded input lacks maxlength=5:\n%s", body)
	}
	for _, name := range []string{"Body", "Note"} {
		want := fmt.Sprintf(`<input id="field-%s" type="text" name="%s" value="" class="input">`, name, name)
		if !strings.Contains(body, want) {
			t.Errorf("%s no longer renders exactly as before; want %s in:\n%s", name, want, body)
		}
	}
	if strings.Contains(body, "<textarea") {
		t.Error("a string field rendered a textarea")
	}
}

func TestCreateAtTheBoundaryStoresAndOneOverIsRefusedKeepingOtherInput(t *testing.T) {
	handler, sqlDB := buildHeadlineAdmin(t)

	// Five runes: accents and an emoji are one rune each, though 6 and 8 bytes.
	for _, title := range []string{"héllo", "ab😀cd", "12345"} {
		resp := doRequest(t, handler, http.MethodPost, headlineBasePath+"new/", url.Values{"Title": {title}, "Body": {"b"}, "Note": {"n"}})
		if resp.Code != http.StatusFound {
			t.Fatalf("title %q: status %d, want %d:\n%s", title, resp.Code, http.StatusFound, resp.Body.String())
		}
	}
	if got := headlineCount(t, sqlDB); got != 3 {
		t.Fatalf("rows = %d, want 3", got)
	}

	resp := doRequest(t, handler, http.MethodPost, headlineBasePath+"new/", url.Values{"Title": {"héllo!"}, "Body": {"keep this body"}, "Note": {"and this note"}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.Code)
	}
	for _, want := range []string{"Title must be at most 5 characters (got 6)", `value="héllo!"`, `value="keep this body"`, `value="and this note"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %q:\n%s", want, body)
		}
	}
	if got := headlineCount(t, sqlDB); got != 3 {
		t.Fatalf("a refused save wrote a row: %d rows", got)
	}
}

func TestEditRefusesAnOverLongValueAndLeavesTheRow(t *testing.T) {
	handler, sqlDB := buildHeadlineAdmin(t)
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_headline (title, body, note) VALUES ($1, $2, $3) RETURNING id`, "old", "b", "n")

	resp := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", headlineBasePath, id), url.Values{"Title": {"toolong"}, "Body": {"new body"}, "Note": {"n"}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "Title must be at most 5 characters (got 7)") || !strings.Contains(body, `value="new body"`) {
		t.Fatalf("status %d, body:\n%s", resp.Code, body)
	}
	var title, stored string
	if err := sqlDB.QueryRow(`SELECT title, body FROM admin_headline WHERE id = $1`, id).Scan(&title, &stored); err != nil || title != "old" || stored != "b" {
		t.Fatalf("row = %q, %q, %v; want it unchanged", title, stored, err)
	}

	ok := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", headlineBasePath, id), url.Values{"Title": {"fits!"}, "Body": {"b"}, "Note": {"n"}})
	if ok.Code != http.StatusFound {
		t.Fatalf("a value at the limit: status %d", ok.Code)
	}
}

func TestOverLongMessageIsTranslatable(t *testing.T) {
	handler, _ := buildHeadlineAdmin(t, admin.WithMiddleware(adminI18NMiddleware()))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.error.too_long": "%s admite como máximo %d caracteres (hay %d)",
	})
	resp := doRequest(t, handler, http.MethodPost, headlineBasePath+"new/", url.Values{"Title": {"demasiado"}})
	if body := resp.Body.String(); !strings.Contains(body, "Title admite como máximo 5 caracteres (hay 9)") {
		t.Fatalf("message not translated:\n%s", body)
	}
}

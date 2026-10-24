package admin_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func TestBulkDeleteRejectsInvalidSelectionsWholesale(t *testing.T) {
	type bad struct {
		name  string
		count int
		keys  func(good []string) []string
	}
	cases := []bad{
		{"empty selection with count zero", 0, func([]string) []string { return nil }},
		{"empty selection with count one", 1, func([]string) []string { return nil }},
		{"count smaller than keys", 1, func(g []string) []string { return g }},
		{"count larger than keys", 5, func(g []string) []string { return g }},
		{"duplicate key", 2, func(g []string) []string { return []string{g[0], g[0]} }},
		{"unparseable key", 2, func(g []string) []string { return []string{g[0], "abc"} }},
		{"empty key", 2, func(g []string) []string { return []string{g[0], ""} }},
		{"leading zero", 2, func(g []string) []string { return []string{g[0], "0" + g[1]} }},
		{"plus sign", 2, func(g []string) []string { return []string{g[0], "+" + g[1]} }},
		{"negative zero", 2, func(g []string) []string { return []string{g[0], "-0"} }},
		{"whitespace", 2, func(g []string) []string { return []string{g[0], " " + g[1]} }},
		{"above int64", 2, func(g []string) []string { return []string{g[0], "9223372036854775808"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			restore := captureDefaultLog(&logs)
			defer restore()
			handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
			good := idStrings(seedProduct(t, sqlDB, "A", 1), seedProduct(t, sqlDB, "B", 2))
			keys := tc.keys(good)

			response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkFormOf(tc.count, keys...))

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %q", response.Code, response.Body.String())
			}
			if location := response.Header().Get("Location"); location != "" {
				t.Errorf("Location = %q, want none", location)
			}
			if got := countProducts(t, sqlDB); got != 2 {
				t.Errorf("products left = %d, want 2", got)
			}
			if strings.Contains(logs.String(), admin.EventBulkDelete) {
				t.Errorf("a rejected action logged a deletion event: %s", logs.String())
			}
		})
	}
}

func TestBulkDeleteRejectsKeysOutsideTheFieldWidth(t *testing.T) {
	handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkTicket{}))
	if _, err := sqlDB.Exec(`INSERT INTO bulk_ticket (id, label) VALUES (44, 'x'), (1, 'y')`); err != nil {
		t.Fatal(err)
	}
	// 300 would wrap to 44 in an int8-style conversion; an int32 field must
	// still refuse a value beyond its own width.
	response := doRequest(t, handler, http.MethodPost, "/admin/bulk_ticket/delete-selected/", bulkFormOf(2, "1", "2147483648"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if got := countRows(t, sqlDB, "bulk_ticket"); got != 2 {
		t.Fatalf("rows left = %d, want 2", got)
	}
}

func TestBulkDeleteRejectsNegativeAndOversizedUnsignedKeys(t *testing.T) {
	handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkCounter{}))
	if _, err := sqlDB.Exec(`INSERT INTO bulk_counter (id, label) VALUES (1, 'x'), (2, 'y')`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"-1", "18446744073709551615"} {
		response := doRequest(t, handler, http.MethodPost, "/admin/bulk_counter/delete-selected/", bulkFormOf(2, "1", bad))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("key %q: status = %d, want 400", bad, response.Code)
		}
	}
	if got := countRows(t, sqlDB, "bulk_counter"); got != 2 {
		t.Fatalf("rows left = %d, want 2", got)
	}
}

func TestBulkDeleteRejectsAnEmptyStringKey(t *testing.T) {
	handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkTag{}))
	if _, err := sqlDB.Exec(`INSERT INTO bulk_tag (slug, label) VALUES ('a', 'x')`); err != nil {
		t.Fatal(err)
	}
	response := doRequest(t, handler, http.MethodPost, "/admin/bulk_tag/delete-selected/", bulkFormOf(2, "a", ""))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if got := countRows(t, sqlDB, "bulk_tag"); got != 1 {
		t.Fatalf("rows left = %d, want 1", got)
	}
}

func TestBulkDeleteCapIsEnforcedBeforeAnyDeletion(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	ids := make([]int64, 0, 26)
	for i := 0; i < 26; i++ {
		ids = append(ids, seedProduct(t, sqlDB, "P"+strconv.Itoa(i), float64(i)))
	}

	over := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkFormOf(26, idStrings(ids...)...))
	if over.Code != http.StatusBadRequest {
		t.Fatalf("26 keys: status = %d, want 400", over.Code)
	}
	if got := countProducts(t, sqlDB); got != 26 {
		t.Fatalf("products left = %d, want 26", got)
	}

	exact := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkFormOf(25, idStrings(ids[:25]...)...))
	if exact.Code != http.StatusFound {
		t.Fatalf("25 keys: status = %d, want 302; body %q", exact.Code, exact.Body.String())
	}
	if got := countProducts(t, sqlDB); got != 1 {
		t.Fatalf("products left = %d, want 1", got)
	}
}

func TestBulkDeleteLogsOneEventWithoutKeysOrValues(t *testing.T) {
	var logs bytes.Buffer
	restore := captureDefaultLog(&logs)
	defer restore()
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "Secret widget", 1)
	second := seedProduct(t, sqlDB, "B", 2)

	response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkForm(2, first, second))
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}

	text := logs.String()
	if got := strings.Count(text, admin.EventBulkDelete); got != 1 {
		t.Fatalf("event logged %d times, want once: %s", got, text)
	}
	for _, want := range []string{"model=crudProduct", "count=2", "username="} {
		if !strings.Contains(text, want) {
			t.Errorf("log lacks %q: %s", want, text)
		}
	}
	if strings.Contains(text, "Secret widget") {
		t.Errorf("log carries a row value: %s", text)
	}
}

func TestBulkDeleteRedirectKeepsAValidatedReturnTo(t *testing.T) {
	cases := []struct {
		name     string
		returnTo string
		want     string
	}{
		{"none", "", "/admin/crud_product/?deleted=2"},
		{"search and page", "/admin/crud_product/?q=a%26b&page=3", "/admin/crud_product/?deleted=2&page=3&q=a%26b"},
		{"other model", "/admin/other/?page=2", "/admin/crud_product/?deleted=2"},
		{"absolute url", "https://evil.example/admin/crud_product/", "/admin/crud_product/?deleted=2"},
		{"scheme relative", "//evil.example/admin/crud_product/", "/admin/crud_product/?deleted=2"},
		{"unknown parameter", "/admin/crud_product/?next=http://evil.example", "/admin/crud_product/?deleted=2"},
		{"repeated page", "/admin/crud_product/?page=1&page=2", "/admin/crud_product/?deleted=2"},
		{"zero page", "/admin/crud_product/?page=0", "/admin/crud_product/?deleted=2"},
		{"non numeric page", "/admin/crud_product/?page=x", "/admin/crud_product/?deleted=2"},
		{"preexisting deleted", "/admin/crud_product/?deleted=9", "/admin/crud_product/?deleted=2"},
		{"fragment", "/admin/crud_product/?page=2#x", "/admin/crud_product/?deleted=2"},
		{"extra path", "/admin/crud_product/new/", "/admin/crud_product/?deleted=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
			first := seedProduct(t, sqlDB, "A", 1)
			second := seedProduct(t, sqlDB, "B", 2)
			form := bulkForm(2, first, second)
			if tc.returnTo != "" {
				form.Set("return_to", tc.returnTo)
			}

			response := doRequest(t, handler, http.MethodPost, bulkDeletePath, form)

			if response.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Location"); got != tc.want {
				t.Errorf("Location = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBulkDeleteReturnToKeepsRegisteredFilters(t *testing.T) {
	handler, sqlDB, _ := buildBulkAdmin(t,
		bulkSpec{value: flAuthor{}, options: admin.Options{ListDisplay: []string{"Name"}, BulkDelete: true}},
		bulkSpec{value: flBook{}, options: admin.Options{ListDisplay: []string{"Title"}, Filters: []string{"Published", "AuthorID"}, BulkDelete: true}},
	)
	author := seedFilterAuthor(t, sqlDB, "Ada")
	one := seedBook(t, sqlDB, "One", true, author)
	two := seedBook(t, sqlDB, "Two", true, author)
	returnTo := "/admin/fl_book/?filter_published=true&q=T&page=1"
	form := bulkForm(2, one, two)
	form.Set("return_to", returnTo)

	response := doRequest(t, handler, http.MethodPost, "/admin/fl_book/delete-selected/", form)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
	}
	got, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/admin/fl_book/" {
		t.Errorf("path = %q", got.Path)
	}
	query := got.Query()
	for key, want := range map[string]string{"filter_published": "true", "q": "T", "page": "1", "deleted": "2"} {
		if query.Get(key) != want {
			t.Errorf("%s = %q, want %q (Location %q)", key, query.Get(key), want, got.String())
		}
	}
}

func idStrings(ids ...int64) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatInt(id, 10)
	}
	return out
}

func captureDefaultLog(buf *bytes.Buffer) func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	return func() { slog.SetDefault(previous) }
}

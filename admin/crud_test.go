package admin_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	_ "modernc.org/sqlite"
)

type crudProduct struct {
	ID    int64 `tango:"pk"`
	Name  string
	Price float64
}

const crudBasePath = "/admin/crud_product/"

func buildProductAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	return buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Search:      []string{"Name"},
		Ordering:    []string{"Name"},
	})
}

func buildProductAdminWithOptions(t *testing.T, options admin.Options) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(crudProduct{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(crudProduct{}, options); err != nil {
		t.Fatalf("register admin model: %v", err)
	}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE crud_product (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, price REAL NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	store := db.NewStore(sqlDB, db.SQLite)
	if err := registry.Register(admin.New(store, admin.Credentials{Username: "admin", Password: "secret"})); err != nil {
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

func seedProduct(t *testing.T, sqlDB *sql.DB, name string, price float64) int64 {
	t.Helper()

	result, err := sqlDB.Exec(`INSERT INTO crud_product (name, price) VALUES (?, ?)`, name, price)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("seed product id: %v", err)
	}
	return id
}

func countProducts(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM crud_product`).Scan(&count); err != nil {
		t.Fatalf("count products: %v", err)
	}
	return count
}

func productExists(t *testing.T, sqlDB *sql.DB, id int64) bool {
	t.Helper()

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM crud_product WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("check product exists: %v", err)
	}
	return count > 0
}

func getProduct(t *testing.T, sqlDB *sql.DB, id int64) crudProduct {
	t.Helper()

	var product crudProduct
	if err := sqlDB.QueryRow(`SELECT id, name, price FROM crud_product WHERE id = ?`, id).Scan(&product.ID, &product.Name, &product.Price); err != nil {
		t.Fatalf("get product: %v", err)
	}
	return product
}

func doRequest(t *testing.T, handler http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.SetBasicAuth("admin", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// --- List view (ticket 06) ---

func TestListViewRendersListDisplayColumns(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Widget", 9.99)

	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "Widget") {
		t.Fatalf("body does not contain seeded product name: %s", response.Body.String())
	}
}

func TestListViewPaginatesResults(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	for i := 1; i <= 30; i++ {
		seedProduct(t, sqlDB, fmt.Sprintf("Item%02d", i), float64(i))
	}

	page1 := doRequest(t, handler, http.MethodGet, crudBasePath+"?page=1", nil)
	page2 := doRequest(t, handler, http.MethodGet, crudBasePath+"?page=2", nil)

	if !strings.Contains(page1.Body.String(), "Item01") {
		t.Fatal("page 1 does not contain Item01")
	}
	if strings.Contains(page1.Body.String(), "Item26") {
		t.Fatal("page 1 unexpectedly contains Item26")
	}
	if !strings.Contains(page2.Body.String(), "Item26") {
		t.Fatal("page 2 does not contain Item26")
	}
}

func TestListViewExactPageSizeShowsNoPhantomNextPage(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	for i := 1; i <= 25; i++ {
		seedProduct(t, sqlDB, fmt.Sprintf("Item%02d", i), float64(i))
	}

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"?page=1", nil)
	body := response.Body.String()

	if !strings.Contains(body, "25 results") {
		t.Fatalf("body does not report the true total count:\n%s", body)
	}
	if strings.Contains(body, "Next page") {
		t.Fatalf("body offers a next page when all 25 rows already fit on page 1:\n%s", body)
	}
}

func TestListViewManyPagesShowsPageLinksAndTotalCount(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	for i := 1; i <= 100; i++ {
		seedProduct(t, sqlDB, fmt.Sprintf("Item%03d", i), float64(i))
	}

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"?page=1", nil)
	body := response.Body.String()

	if !strings.Contains(body, "100 results") {
		t.Fatalf("body does not report the true total count:\n%s", body)
	}
	if !strings.Contains(body, "Next page") {
		t.Fatalf("page 1 of 4 should offer a next page:\n%s", body)
	}
	for _, page := range []string{"?page=2", "?page=3", "?page=4"} {
		if !strings.Contains(body, page) {
			t.Fatalf("expected a link to %s among the page links:\n%s", page, body)
		}
	}
}

func TestListViewDefaultsToPageOne(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Solo", 1)

	withoutPage := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	withPage := doRequest(t, handler, http.MethodGet, crudBasePath+"?page=1", nil)

	if withoutPage.Body.String() != withPage.Body.String() {
		t.Fatal("default page differs from explicit page=1")
	}
}

func TestListViewEmptyModelRendersWithoutError(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

// --- Create view (ticket 08) ---

func TestCreateViewRendersInputsForEditableFields(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"new/", nil)
	body := response.Body.String()
	if !strings.Contains(body, `name="Name"`) || !strings.Contains(body, `name="Price"`) {
		t.Fatalf("form missing editable field inputs: %s", body)
	}
	if strings.Contains(body, `name="ID"`) {
		t.Fatal("form unexpectedly renders the primary key field")
	}
}

func TestCreateViewValidSubmissionCreatesRowAndRedirects(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodPost, crudBasePath+"new/", url.Values{
		"Name":  {"New Product"},
		"Price": {"12.5"},
	})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if countProducts(t, sqlDB) != 1 {
		t.Fatalf("product count = %d, want 1", countProducts(t, sqlDB))
	}
}

func TestCreateViewMalformedSubmissionReRendersWithError(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodPost, crudBasePath+"new/", url.Values{
		"Name":  {"Broken"},
		"Price": {"not-a-number"},
	})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if countProducts(t, sqlDB) != 0 {
		t.Fatal("a row was created despite malformed submission")
	}
}

func TestCreateViewIgnoresSubmittedPrimaryKey(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodPost, crudBasePath+"new/", url.Values{
		"ID":    {"999"},
		"Name":  {"Ignored PK"},
		"Price": {"1"},
	})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if productExists(t, sqlDB, 999) {
		t.Fatal("submitted primary key 999 was used, want autoincrement")
	}
}

// --- Search (ticket 14) ---

func TestSearchFiltersRowsMatchingSearchFields(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Hammer", 10)
	seedProduct(t, sqlDB, "Screwdriver", 5)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=Hammer", nil)
	body := response.Body.String()
	if !strings.Contains(body, "Hammer") {
		t.Fatal("search result missing matching row")
	}
	if strings.Contains(body, "Screwdriver") {
		t.Fatal("search result contains non-matching row")
	}
}

func TestSearchNoMatchReturnsEmptyResults(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Hammer", 10)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=NoSuchThing", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "Hammer") {
		t.Fatal("body unexpectedly contains a non-matching row")
	}
}

func TestSearchEmptyQueryReturnsUnfilteredList(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Hammer", 10)

	withQuery := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=", nil)
	withoutQuery := doRequest(t, handler, http.MethodGet, crudBasePath, nil)

	if !strings.Contains(withQuery.Body.String(), "Hammer") {
		t.Fatal("empty query unexpectedly filtered out row")
	}
	if !strings.Contains(withoutQuery.Body.String(), "Hammer") {
		t.Fatal("unfiltered list missing row")
	}
}

func TestSearchComposesWithPagination(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	for i := 1; i <= 30; i++ {
		seedProduct(t, sqlDB, fmt.Sprintf("Tool%02d", i), float64(i))
	}

	page1 := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=Tool&page=1", nil)
	page2 := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=Tool&page=2", nil)

	if !strings.Contains(page1.Body.String(), "Tool01") {
		t.Fatal("search page 1 missing expected row")
	}
	if strings.Contains(page1.Body.String(), "Tool26") {
		t.Fatal("search page 1 unexpectedly contains page 2's row")
	}
	if !strings.Contains(page2.Body.String(), "Tool26") {
		t.Fatal("search page 2 missing expected row")
	}
}

func TestSearchWithNoSearchFieldsIsNoOp(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
	})
	seedProduct(t, sqlDB, "Hammer", 10)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"?q=NoMatch", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "Hammer") {
		t.Fatal("query with no search fields unexpectedly filtered out row")
	}
}

// --- Edit view (ticket 10) ---

func TestEditViewPrefillsFormWithCurrentValues(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Existing", 5)

	response := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/", crudBasePath, id), nil)
	body := response.Body.String()

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(body, `value="Existing"`) {
		t.Fatalf("form missing current name value: %s", body)
	}
	if !strings.Contains(body, `value="5"`) {
		t.Fatalf("form missing current price value: %s", body)
	}
	if strings.Contains(body, `name="ID"`) {
		t.Fatal("form unexpectedly renders the primary key field")
	}
}

func TestEditViewUnknownPrimaryKeyReturns404(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := doRequest(t, handler, method, crudBasePath+"999/", url.Values{
			"Name":  {"Missing"},
			"Price": {"1"},
		})

		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", method, response.Code, http.StatusNotFound)
		}
	}
}

func TestEditViewValidSubmissionUpdatesRowAndRedirects(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Old", 5)

	response := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", crudBasePath, id), url.Values{
		"Name":  {"Updated"},
		"Price": {"12.5"},
	})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if location := response.Header().Get("Location"); location != crudBasePath {
		t.Fatalf("Location = %q, want %q", location, crudBasePath)
	}

	product := getProduct(t, sqlDB, id)
	if product.Name != "Updated" || product.Price != 12.5 {
		t.Fatalf("product = %+v, want updated name and price", product)
	}
}

func TestEditViewMalformedSubmissionReRendersWithError(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Original", 5)

	response := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", crudBasePath, id), url.Values{
		"Name":  {"Submitted"},
		"Price": {"not-a-number"},
	})
	body := response.Body.String()

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(body, "Price") {
		t.Fatalf("body missing field error: %s", body)
	}
	if !strings.Contains(body, `value="Submitted"`) {
		t.Fatalf("body missing submitted value: %s", body)
	}

	product := getProduct(t, sqlDB, id)
	if product.Name != "Original" || product.Price != 5 {
		t.Fatalf("product = %+v, want original values unchanged", product)
	}
}

// --- Delete view (ticket 12) ---

func TestDeleteViewGetRendersConfirmationWithoutDeleting(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Disposable", 1)

	response := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil)
	body := response.Body.String()

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(body, "Delete crudProduct") || !strings.Contains(body, fmt.Sprintf("crudProduct %d", id)) {
		t.Fatalf("body missing delete confirmation: %s", body)
	}
	if !productExists(t, sqlDB, id) {
		t.Fatal("GET delete page removed the row")
	}
}

func TestDeleteViewUnknownPrimaryKeyReturns404(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := doRequest(t, handler, method, crudBasePath+"999/delete/", nil)

		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", method, response.Code, http.StatusNotFound)
		}
	}
}

func TestDeleteViewPostRemovesRowAndRedirects(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Disposable", 1)

	response := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusFound)
	}
	if location := response.Header().Get("Location"); location != crudBasePath {
		t.Fatalf("Location = %q, want %q", location, crudBasePath)
	}
	if productExists(t, sqlDB, id) {
		t.Fatal("deleted product still exists")
	}
}

func TestDeleteViewPostOnAlreadyDeletedRowReturns404(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Disposable", 1)
	path := fmt.Sprintf("%s%d/delete/", crudBasePath, id)

	first := doRequest(t, handler, http.MethodPost, path, nil)
	if first.Code != http.StatusFound {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusFound)
	}

	second := doRequest(t, handler, http.MethodPost, path, nil)
	if second.Code != http.StatusNotFound {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusNotFound)
	}
}

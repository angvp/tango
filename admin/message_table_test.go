package admin_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// messageBook carries one field of each kind the admin reports on: a unique
// title with a length limit, a number, a decimal and a JSON document.
type messageBook struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"unique,varchar=12"`
	Year  int64
	Price float64
	Notes string `tango:"json"`
}

const messageBookBasePath = "/admin/message_book/"

func buildMessageBookAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(messageBook{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(messageBook{}, admin.Options{
		ListDisplay:  []string{"Title", "Year"},
		SingularName: "book",
		PluralName:   "books",
	}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}

	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
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

func seedMessageBook(t *testing.T, sqlDB *sql.DB, title string) {
	t.Helper()
	if _, err := sqlDB.Exec(`INSERT INTO message_book (title, year, price, notes) VALUES ($1, $2, $3, $4)`, title, 1999, 1.5, ""); err != nil {
		t.Fatalf("seed message book: %v", err)
	}
}

func TestCreateViewDuplicateUniqueValueIsAFieldErrorNotAServerError(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")

	response := doRequest(t, handler, http.MethodPost, messageBookBasePath+"new/", url.Values{
		"Title": {"Dune"},
		"Year":  {"2024"},
		"Price": {"9.5"},
		"Notes": {""},
	})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %.300s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "Another book already uses this title") {
		t.Fatalf("body lacks the field message for the duplicate title: %s", body)
	}
	if !strings.Contains(body, `value="2024"`) || !strings.Contains(body, `value="9.5"`) {
		t.Fatalf("the failed save did not keep the other submitted values: %.300s", body)
	}
}

// TestCreateViewUnattributableUniqueViolationIsAFormMessageNotAServerError
// covers a unique index the model does not declare: the database names a
// column no field owns, so the refusal cannot be shown beside a field.
func TestCreateViewUnattributableUniqueViolationIsAFormMessageNotAServerError(t *testing.T) {
	handler, sqlDB := buildMessageBookAdmin(t)
	seedMessageBook(t, sqlDB, "Dune")
	if _, err := sqlDB.Exec(`CREATE UNIQUE INDEX message_book_year_only ON message_book (year)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`UPDATE message_book SET year = 2024`); err != nil {
		t.Fatal(err)
	}

	response := doRequest(t, handler, http.MethodPost, messageBookBasePath+"new/", url.Values{
		"Title": {"Dune II"},
		"Year":  {"2024"},
		"Price": {"9.5"},
		"Notes": {""},
	})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %.300s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "Another book already uses one of these values.") {
		t.Fatalf("body lacks the form-level message: %.300s", body)
	}
	for _, leak := range []string{"UNIQUE", "constraint", "message_book.year", "internal error"} {
		if strings.Contains(body, leak) {
			t.Fatalf("body leaks %q: %.300s", leak, body)
		}
	}
}

package admin_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// messageBook carries one field of each kind the admin reports on: a unique
// title with a length limit, a number, a decimal and a JSON document.
type messageBook struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique,varchar=12"`
	Year      int64
	Price     float64
	Notes     string `tango:"json"`
	Published time.Time
}

const messageBookBasePath = "/admin/message_book/"

func buildMessageBookAdmin(t *testing.T, adminOpts ...admin.Option) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(messageBook{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(messageBook{}, admin.Options{
		ListDisplay:  []string{"Title", "Year"},
		Search:       []string{"Title"},
		SingularName: "book",
		PluralName:   "books",
		BulkDelete:   true,
	}); err != nil {
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

func seedMessageBook(t *testing.T, sqlDB *sql.DB, title string) int64 {
	t.Helper()
	return insertReturningID(t, sqlDB, `INSERT INTO message_book (title, year, price, notes, published) VALUES ($1, $2, $3, $4, $5) RETURNING id`, title, 1999, 1.5, "", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
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

// messageTableForbidden lists text that must never reach a user-error page:
// Go error text, SQL, decoder output and the server-failure wording.
var messageTableForbidden = []string{
	"strconv", "invalid syntax", `field "`, "ParseFloat", "ValueTooLong", "db:",
	"UNIQUE", "constraint", "FOREIGN KEY", "internal error",
	"invalid character", "unexpected end", `{"error"`,
}

func TestBadInputYieldsFriendlyMessagesOnCreateAndEdit(t *testing.T) {
	type row struct {
		name     string
		form     url.Values
		expected string
		keeps    []string
	}
	base := func(overrides url.Values) url.Values {
		form := url.Values{"Title": {"Atlas"}, "Year": {"2024"}, "Price": {"9.5"}, "Notes": {""}, "Published": {"2024-05-01T10:30"}}
		for key, values := range overrides {
			form[key] = values
		}
		return form
	}
	rows := []row{
		{name: "year is not a number", form: base(url.Values{"Year": {"abc"}}), expected: "Year must be a whole number.", keeps: []string{`value="abc"`, `value="9.5"`, `value="Atlas"`}},
		{name: "price is not a number", form: base(url.Values{"Price": {"x"}}), expected: "Price must be a number.", keeps: []string{`value="x"`, `value="2024"`}},
		{name: "published is not a date", form: base(url.Values{"Published": {"someday"}}), expected: "Published must be a date and time.", keeps: []string{`value="someday"`}},
		{name: "title is too long", form: base(url.Values{"Title": {"A very long title"}}), expected: "Title must be at most 12 characters", keeps: []string{`value="2024"`}},
		{name: "notes is not JSON", form: base(url.Values{"Notes": {"{"}}), expected: "This is not valid JSON", keeps: []string{`value="2024"`}},
		{name: "title already used", form: base(url.Values{"Title": {"Dune"}}), expected: "Another book already uses this title.", keeps: []string{`value="2024"`, `value="9.5"`}},
	}
	for _, tt := range rows {
		for _, mode := range []string{"create", "edit"} {
			t.Run(mode+" "+tt.name, func(t *testing.T) {
				handler, sqlDB := buildMessageBookAdmin(t)
				seedMessageBook(t, sqlDB, "Dune")
				target := messageBookBasePath + "new/"
				if mode == "edit" {
					id := seedMessageBook(t, sqlDB, "Emma")
					target = messageBookBasePath + strconv.FormatInt(id, 10) + "/"
				}
				response := doRequest(t, handler, http.MethodPost, target, tt.form)

				body := response.Body.String()
				if response.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422; body %.300s", response.Code, body)
				}
				if !strings.Contains(body, tt.expected) {
					t.Fatalf("body lacks %q", tt.expected)
				}
				for _, text := range messageTableForbidden {
					if strings.Contains(body, text) {
						t.Fatalf("body contains forbidden %q", text)
					}
				}
				for _, text := range tt.keeps {
					if !strings.Contains(body, text) {
						t.Fatalf("body lost submitted value %q", text)
					}
				}
			})
		}
	}
}

// TestSaveWithMissingForeignKeyTargetIsAFormMessage covers a save whose
// foreign key names a row that does not exist: the refusal is a form-level
// message with no database text, and the page is a 422, not a server error.
func TestSaveWithMissingForeignKeyTargetIsAFormMessage(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	seedAuthor(t, sqlDB, "Ursula")

	response := doRequest(t, handler, http.MethodPost, "/admin/fk_post/new/", url.Values{
		"Title":    {"A post"},
		"AuthorID": {"999"},
	})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %.300s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "The item you chose no longer exists. Choose another and save again.") {
		t.Fatalf("body lacks the form-level message: %.300s", body)
	}
	for _, leak := range []string{"FOREIGN KEY", "constraint", "nonexistent", "internal error", "tango db"} {
		if strings.Contains(body, leak) {
			t.Fatalf("body leaks %q", leak)
		}
	}
}

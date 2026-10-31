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
)

type uniqueItem struct {
	ID   int64  `tango:"pk"`
	Code string `tango:"unique"`
}

func buildUniqueItemAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(uniqueItem{}); err != nil {
		t.Fatalf("register uniqueItem: %v", err)
	}
	if err := registry.Admin().Register(uniqueItem{}, admin.Options{ListDisplay: []string{"Code"}}); err != nil {
		t.Fatalf("register admin uniqueItem: %v", err)
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

// Saving a value another row already holds is refused: the edit form comes
// back with status 422 and a message on the field, and the row keeps its
// value.
func TestEditViewSaveDuplicateUniqueValueRerendersWithFieldError(t *testing.T) {
	handler, sqlDB := buildUniqueItemAdmin(t)
	insertReturningID(t, sqlDB, `INSERT INTO unique_item (code) VALUES ($1) RETURNING id`, "alpha")
	betaID := insertReturningID(t, sqlDB, `INSERT INTO unique_item (code) VALUES ($1) RETURNING id`, "beta")

	response := doRequest(t, handler, http.MethodPost, fmt.Sprintf("/admin/unique_item/%d/", betaID), url.Values{
		"Code": {"alpha"},
	})
	body := response.Body.String()

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(body, `name="Code"`) || !strings.Contains(body, "Another unique item already uses this value.") {
		t.Fatalf("body missing the field error on Code: %s", body)
	}
	var code string
	if err := sqlDB.QueryRow(`SELECT code FROM unique_item WHERE id = $1`, betaID).Scan(&code); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if code != "beta" {
		t.Fatalf("code = %q after refused save, want %q", code, "beta")
	}
}

func TestEditViewRejectsUnsupportedMethod(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Widget", 2)

	response := doRequest(t, handler, http.MethodPut, fmt.Sprintf("%s%d/", crudBasePath, id), url.Values{
		"name": {"Changed"},
	})

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := getProduct(t, sqlDB, id).Name; got != "Widget" {
		t.Fatalf("name = %q after rejected PUT, want %q", got, "Widget")
	}
}

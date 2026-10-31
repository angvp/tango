package admin_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

// A row that a table outside the admin registry still references cannot be
// deleted: Store.Delete cascades only through registered models, so the
// database refuses the delete, and the admin answers 409 with the reason on
// the confirmation page instead of a server error.
func TestDeleteViewPostOnReferencedRowIsRefusedWithConflict(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	authorID := seedAuthor(t, sqlDB, "Jane Doe")
	if _, err := sqlDB.Exec(`CREATE TABLE fk_unmanaged_note (id INTEGER PRIMARY KEY, author_id INTEGER REFERENCES fk_author (id))`); err != nil {
		t.Fatalf("create unregistered referencing table: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO fk_unmanaged_note (id, author_id) VALUES (1, $1)`, authorID); err != nil {
		t.Fatalf("insert referencing note: %v", err)
	}

	response := doRequest(t, handler, http.MethodPost, fmt.Sprintf("/admin/fk_author/%d/delete/", authorID), nil)
	body := response.Body.String()

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	const reason = "This fk author cannot be deleted because other rows still use it. Remove or change those rows first."
	if !strings.Contains(body, reason) {
		t.Fatalf("body missing refusal reason %q: %s", reason, body)
	}
	if got := countRows(t, sqlDB, "fk_author"); got != 1 {
		t.Fatalf("fk_author rows = %d, want 1 (refused delete must not remove the row)", got)
	}
}

func TestDeleteViewRejectsUnsupportedMethod(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Disposable", 1)

	response := doRequest(t, handler, http.MethodPut, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if !productExists(t, sqlDB, id) {
		t.Fatal("rejected PUT removed the row")
	}
}

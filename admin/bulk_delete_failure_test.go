package admin_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
)

// bulkRunDialect reports the dialect this run's TANGO_TEST_DSN selects, which
// is SQLite when the variable is unset, matching testdb.
func bulkRunDialect(t *testing.T) db.Dialect {
	t.Helper()
	dsn := os.Getenv("TANGO_TEST_DSN")
	if dsn == "" {
		return db.SQLite
	}
	parsed, err := db.ParseDSN(dsn)
	if err != nil {
		t.Fatal("TANGO_TEST_DSN does not parse")
	}
	return parsed.Dialect
}

// blockCrudProductDelete installs a BEFORE DELETE trigger on crud_product.
// With ignore set, the delete of rows matching onlyID (every row when onlyID
// is zero) is skipped silently; otherwise it is refused with an error.
func blockCrudProductDelete(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, onlyID int64, ignore bool) {
	t.Helper()
	var stmts []string
	switch dialect {
	case db.SQLite:
		when := ""
		if onlyID != 0 {
			when = fmt.Sprintf(" WHEN OLD.id = %d", onlyID)
		}
		action := `SELECT RAISE(ABORT, 'delete blocked for test');`
		if ignore {
			action = `SELECT RAISE(IGNORE);`
		}
		stmts = []string{`CREATE TRIGGER block_crud_product_delete BEFORE DELETE ON crud_product` + when + ` BEGIN ` + action + ` END;`}
	case db.Postgres:
		body := `RAISE EXCEPTION 'delete blocked for test';`
		if ignore {
			body = `RETURN NULL;`
		}
		when := ""
		if onlyID != 0 {
			when = fmt.Sprintf(" WHEN (OLD.id = %d)", onlyID)
		}
		stmts = []string{
			`CREATE FUNCTION block_crud_product_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + body + ` END; $$`,
			`CREATE TRIGGER block_crud_product_delete BEFORE DELETE ON crud_product FOR EACH ROW` + when + ` EXECUTE FUNCTION block_crud_product_delete()`,
		}
	default:
		t.Skipf("no delete-blocking trigger is written for dialect %v in this test", dialect)
	}
	for _, stmt := range stmts {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("install trigger: %v", err)
		}
	}
}

func TestBulkDeleteFailingMidTransactionLeavesEveryRowInPlace(t *testing.T) {
	dialect := bulkRunDialect(t)
	cases := []struct {
		name    string
		table   string
		prepare func(t *testing.T, sqlDB *sql.DB, second int64)
	}{
		{
			name:  "selection query fails",
			table: "crud_product_moved",
			prepare: func(t *testing.T, sqlDB *sql.DB, _ int64) {
				t.Helper()
				if _, err := sqlDB.Exec(`ALTER TABLE crud_product RENAME TO crud_product_moved`); err != nil {
					t.Fatalf("rename: %v", err)
				}
			},
		},
		{
			name:  "database refuses every delete",
			table: "crud_product",
			prepare: func(t *testing.T, sqlDB *sql.DB, _ int64) {
				blockCrudProductDelete(t, sqlDB, dialect, 0, false)
			},
		},
		{
			name:  "selected row survives its delete",
			table: "crud_product",
			prepare: func(t *testing.T, sqlDB *sql.DB, second int64) {
				blockCrudProductDelete(t, sqlDB, dialect, second, true)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
			first := seedProduct(t, sqlDB, "A", 1)
			second := seedProduct(t, sqlDB, "B", 2)
			seedProduct(t, sqlDB, "C", 3)
			tc.prepare(t, sqlDB, second)

			response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkForm(2, first, second))

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body %q", response.Code, response.Body.String())
			}
			if got := countRows(t, sqlDB, tc.table); got != 3 {
				t.Fatalf("rows left = %d, want 3: a failed bulk delete must roll back every delete", got)
			}
		})
	}
}

func TestBulkDeleteRejectsAMalformedFormBodyBeforeDeleting(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	id := seedProduct(t, sqlDB, "A", 1)

	request := httptest.NewRequest(http.MethodPost, bulkDeletePath, strings.NewReader("pk=%zz&count=1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an unparseable form body; body %q", response.Code, response.Body.String())
	}
	if !productExists(t, sqlDB, id) {
		t.Errorf("row deleted by a request whose form did not parse")
	}
}

// The route accepts POST only, so a GET is answered by the HTML 405 page that
// lists POST in Allow, and no row may be deleted by it.
func TestBulkDeleteRefusesTheWrongMethodWithoutDeleting(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	id := seedProduct(t, sqlDB, "A", 1)

	response := doRequest(t, handler, http.MethodGet, bulkDeletePath, bulkForm(1, id))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Allow"); got != "POST" {
		t.Fatalf("Allow = %q, want POST", got)
	}
	if !productExists(t, sqlDB, id) {
		t.Errorf("row deleted by a GET")
	}
}

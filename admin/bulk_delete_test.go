package admin_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
)

const bulkDeletePath = crudBasePath + "delete-selected/"

func bulkProductOptions() admin.Options {
	return admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Search:      []string{"Name"},
		Ordering:    []string{"Name"},
		BulkDelete:  true,
	}
}

func bulkForm(count int, ids ...int64) url.Values {
	form := url.Values{"count": {strconv.Itoa(count)}}
	for _, id := range ids {
		form.Add("pk", fmt.Sprint(id))
	}
	return form
}

func TestBulkDeleteRemovesTheSelectedRowsAndRedirectsToTheList(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	first := seedProduct(t, sqlDB, "A", 1)
	second := seedProduct(t, sqlDB, "B", 2)
	kept := seedProduct(t, sqlDB, "C", 3)

	response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkForm(2, first, second))

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body %q", response.Code, http.StatusFound, response.Body.String())
	}
	if productExists(t, sqlDB, first) || productExists(t, sqlDB, second) {
		t.Errorf("a selected product is still present")
	}
	if !productExists(t, sqlDB, kept) {
		t.Errorf("an unselected product was deleted")
	}
}

type bulkSpec struct {
	value   any
	options admin.Options
}

// buildBulkAdmin wires every spec's model with its options through the real
// admin handler on a fresh migrated database.
func buildBulkAdmin(t *testing.T, specs ...bulkSpec) (http.Handler, *sql.DB, *db.Store) {
	t.Helper()
	registry := tango.NewRegistry()
	for _, spec := range specs {
		if err := registry.Models().Register(spec.value); err != nil {
			t.Fatalf("register %T: %v", spec.value, err)
		}
		if err := registry.Admin().Register(spec.value, spec.options); err != nil {
			t.Fatalf("register admin %T: %v", spec.value, err)
		}
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
	return handler, sqlDB, store
}

func countRows(t *testing.T, sqlDB *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func bulkFormOf(count int, keys ...string) url.Values {
	form := url.Values{"count": {strconv.Itoa(count)}}
	for _, key := range keys {
		form.Add("pk", key)
	}
	return form
}

type bulkTag struct {
	Slug  string `tango:"pk"`
	Label string
}

type bulkCounter struct {
	ID    uint64 `tango:"pk"`
	Label string
}

type bulkTicket struct {
	ID    int32 `tango:"pk"`
	Label string
}

type bulkNode struct {
	ID       int64 `tango:"pk"`
	ParentID int64 `tango:"fk=bulkNode"`
	Name     string
}

func bulkOn(value any) bulkSpec {
	return bulkSpec{value: value, options: admin.Options{BulkDelete: true}}
}

func TestBulkDeleteWorksForStringSignedAndUnsignedKeys(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkTag{}))
		for _, slug := range []string{"alpha", "beta", "gamma"} {
			if _, err := sqlDB.Exec(`INSERT INTO bulk_tag (slug, label) VALUES ($1, $2)`, slug, slug); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		response := doRequest(t, handler, http.MethodPost, "/admin/bulk_tag/delete-selected/", bulkFormOf(2, "gamma", "alpha"))
		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
		}
		if got := countRows(t, sqlDB, "bulk_tag"); got != 1 {
			t.Fatalf("rows left = %d, want 1", got)
		}
	})
	t.Run("signed", func(t *testing.T) {
		handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkTicket{}))
		for _, id := range []int{-5, 7, 9} {
			if _, err := sqlDB.Exec(`INSERT INTO bulk_ticket (id, label) VALUES ($1, $2)`, id, "x"); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		response := doRequest(t, handler, http.MethodPost, "/admin/bulk_ticket/delete-selected/", bulkFormOf(2, "-5", "9"))
		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
		}
		if got := countRows(t, sqlDB, "bulk_ticket"); got != 1 {
			t.Fatalf("rows left = %d, want 1", got)
		}
	})
	t.Run("unsigned", func(t *testing.T) {
		handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkCounter{}))
		for _, id := range []int{1, 2, 3} {
			if _, err := sqlDB.Exec(`INSERT INTO bulk_counter (id, label) VALUES ($1, $2)`, id, "x"); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		response := doRequest(t, handler, http.MethodPost, "/admin/bulk_counter/delete-selected/", bulkFormOf(2, "1", "3"))
		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
		}
		if got := countRows(t, sqlDB, "bulk_counter"); got != 1 {
			t.Fatalf("rows left = %d, want 1", got)
		}
	})
}

func TestBulkDeleteRollsBackWhenASelectedKeyHasNoRow(t *testing.T) {
	t.Run("integer", func(t *testing.T) {
		handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
		first := seedProduct(t, sqlDB, "A", 1)
		second := seedProduct(t, sqlDB, "B", 2)

		response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkForm(3, first, second, 9999))

		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body %q", response.Code, response.Body.String())
		}
		if !productExists(t, sqlDB, first) || !productExists(t, sqlDB, second) {
			t.Errorf("a row was deleted although the action failed")
		}
	})
	t.Run("string differing only in trailing space", func(t *testing.T) {
		handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkTag{}))
		if _, err := sqlDB.Exec(`INSERT INTO bulk_tag (slug, label) VALUES ($1, $2), ($3, $4)`, "a", "x", "b", "y"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		response := doRequest(t, handler, http.MethodPost, "/admin/bulk_tag/delete-selected/", bulkFormOf(2, "b", "a "))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body %q", response.Code, response.Body.String())
		}
		if got := countRows(t, sqlDB, "bulk_tag"); got != 2 {
			t.Fatalf("rows left = %d, want 2: nothing may be deleted", got)
		}
	})
}

func TestBulkDeleteCascadesToRowsThatReferenceTheSelection(t *testing.T) {
	handler, sqlDB, _ := buildBulkAdmin(t,
		bulkSpec{value: flAuthor{}, options: admin.Options{ListDisplay: []string{"Name"}, BulkDelete: true}},
		bulkSpec{value: flBook{}, options: admin.Options{ListDisplay: []string{"Title"}}},
	)
	ada := seedFilterAuthor(t, sqlDB, "Ada")
	grace := seedFilterAuthor(t, sqlDB, "Grace")
	keep := seedFilterAuthor(t, sqlDB, "Edsger")
	seedBook(t, sqlDB, "One", true, ada)
	seedBook(t, sqlDB, "Two", true, grace)
	kept := seedBook(t, sqlDB, "Three", true, keep)

	response := doRequest(t, handler, http.MethodPost, "/admin/fl_author/delete-selected/", bulkForm(2, ada, grace))

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
	}
	if got := countRows(t, sqlDB, "fl_author"); got != 1 {
		t.Errorf("authors left = %d, want 1", got)
	}
	if got := countRows(t, sqlDB, "fl_book"); got != 1 {
		t.Errorf("books left = %d, want only the unselected author's book (%d)", got, kept)
	}
}

func TestBulkDeleteOutcomeDoesNotDependOnTheOrderOfOverlappingKeys(t *testing.T) {
	for name, order := range map[string]func(parent, child int64) []int64{
		"parent first": func(parent, child int64) []int64 { return []int64{parent, child} },
		"child first":  func(parent, child int64) []int64 { return []int64{child, parent} },
	} {
		t.Run(name, func(t *testing.T) {
			handler, sqlDB, _ := buildBulkAdmin(t, bulkOn(bulkNode{}))
			parent := insertReturningID(t, sqlDB, `INSERT INTO bulk_node (name, parent_id) VALUES ($1, NULL) RETURNING id`, "root")
			child := insertReturningID(t, sqlDB, `INSERT INTO bulk_node (name, parent_id) VALUES ($1, $2) RETURNING id`, "child", parent)
			grandchild := insertReturningID(t, sqlDB, `INSERT INTO bulk_node (name, parent_id) VALUES ($1, $2) RETURNING id`, "grandchild", child)
			other := insertReturningID(t, sqlDB, `INSERT INTO bulk_node (name, parent_id) VALUES ($1, NULL) RETURNING id`, "other")

			response := doRequest(t, handler, http.MethodPost, "/admin/bulk_node/delete-selected/", bulkForm(2, order(parent, child)...))

			if response.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302; body %q", response.Code, response.Body.String())
			}
			var left []int64
			rows, err := sqlDB.Query(`SELECT id FROM bulk_node ORDER BY id`)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					t.Fatalf("scan: %v", err)
				}
				left = append(left, id)
			}
			if len(left) != 1 || left[0] != other {
				t.Fatalf("rows left = %v, want only %d: the selection and everything referencing it (%d) must be gone", left, other, grandchild)
			}
		})
	}
}

func TestBulkDeleteKeepsTheStaffGateAndCSRFProtection(t *testing.T) {
	setup := func(t *testing.T) (http.Handler, *sql.DB, *db.Store, int64) {
		handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
		id := seedProduct(t, sqlDB, "A", 1)
		return handler, sqlDB, nil, id
	}

	t.Run("no session redirects to login", func(t *testing.T) {
		handler, sqlDB, _, id := setup(t)
		request := httptest.NewRequest(http.MethodPost, bulkDeletePath, strings.NewReader(bulkForm(1, id).Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "/admin/login/") {
			t.Fatalf("status = %d Location = %q, want a redirect to login", response.Code, response.Header().Get("Location"))
		}
		if !productExists(t, sqlDB, id) {
			t.Errorf("row deleted without a session")
		}
	})
	t.Run("missing or wrong CSRF token is refused", func(t *testing.T) {
		handler, sqlDB, _, id := setup(t)
		for _, token := range []string{"", "wrong"} {
			form := bulkForm(1, id)
			form.Set("csrf_token", token)
			request := httptest.NewRequest(http.MethodPost, bulkDeletePath, strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("token %q: status = %d, want 403", token, response.Code)
			}
		}
		if !productExists(t, sqlDB, id) {
			t.Errorf("row deleted with a bad CSRF token")
		}
	})
	t.Run("non-staff account gets 403", func(t *testing.T) {
		registry := tango.NewRegistry()
		if err := registry.Models().Register(crudProduct{}); err != nil {
			t.Fatal(err)
		}
		if err := registry.Admin().Register(crudProduct{}, bulkProductOptions()); err != nil {
			t.Fatal(err)
		}
		sqlDB, store := migratedAdminDB(t, registry)
		seedAdminAccountAndSession(t, sqlDB, store)
		if err := admin.CreateAccount(context.Background(), store, "guest", "secret", admin.WithoutStaff()); err != nil {
			t.Fatal(err)
		}
		if err := registry.Register(admin.New(store)); err != nil {
			t.Fatal(err)
		}
		if err := registry.RunRegistration(); err != nil {
			t.Fatal(err)
		}
		handler, err := registry.Routes().Handler()
		if err != nil {
			t.Fatal(err)
		}
		id := seedProduct(t, sqlDB, "A", 1)
		cookie := loginAndGetSessionCookie(t, handler, "guest", "secret")
		sum := sha256.Sum256([]byte(cookie.Value))
		form := bulkForm(1, id)
		form.Set("csrf_token", hex.EncodeToString(sum[:]))
		request := httptest.NewRequest(http.MethodPost, bulkDeletePath, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", response.Code)
		}
		if !productExists(t, sqlDB, id) {
			t.Errorf("row deleted by a non-staff account")
		}
	})
}

func TestBulkDeleteIsOffUnlessTheModelOptsIn(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "A", 1)

	response := doRequest(t, handler, http.MethodPost, bulkDeletePath, bulkForm(1, id))

	if response.Code == http.StatusFound || response.Code == http.StatusOK {
		t.Fatalf("status = %d: a model without BulkDelete must not serve the route", response.Code)
	}
	if !productExists(t, sqlDB, id) {
		t.Errorf("row deleted by a model that did not opt in")
	}
}

package admin_test

import (
	"database/sql"
	"net/http"
	"regexp"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

type relAdminSupplier struct {
	ID   int64  `tango:"pk"`
	Name string `tango:"varchar=100"`
}

type relAdminPlainItem struct {
	ID         int64  `tango:"pk"`
	SupplierID int64  `tango:"fk=relAdminSupplier"`
	Label      string `tango:"varchar=100"`
}

type relAdminRelatedItem struct {
	ID         int64             `tango:"pk"`
	SupplierID int64             `tango:"fk=relAdminSupplier"`
	Label      string            `tango:"varchar=100"`
	Supplier   *relAdminSupplier `tango:"belongs_to=SupplierID"`
}

type relAdminExtraItem struct {
	ID         int64  `tango:"pk"`
	SupplierID int64  `tango:"fk=relAdminSupplier"`
	Label      string `tango:"varchar=100"`
	Note       string `tango:"varchar=100"`
}

var relAdminHeader = regexp.MustCompile(`<th[^>]*>([^<]*)</th>`)

// changelistHeaders renders the changelist of model and returns its column
// header texts in order.
func changelistHeaders(t *testing.T, value any, options admin.Options, path string, seed func(*testing.T, *sql.DB, int64)) []string {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(relAdminSupplier{}); err != nil {
		t.Fatalf("register supplier: %v", err)
	}
	if err := registry.Models().Register(value); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(value, options); err != nil {
		t.Fatalf("register admin model: %v", err)
	}

	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	// An empty changelist renders no table, so each model gets one row.
	supplierID := insertReturningID(t, sqlDB, `INSERT INTO rel_admin_supplier (name) VALUES ($1) RETURNING id`, "Acme")
	seed(t, sqlDB, supplierID)
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

	body := getWithCookies(t, handler, path, &http.Cookie{Name: "tango_admin_session", Value: testSessionToken}).Body.String()
	var headers []string
	for _, match := range relAdminHeader.FindAllStringSubmatch(body, -1) {
		headers = append(headers, match[1])
	}
	return headers
}

// A relation field adds no changelist column: the page of a model that has one
// shows exactly the headers of the same model without it.
func TestChangelistShowsNoColumnForARelationField(t *testing.T) {
	plain := changelistHeaders(t, relAdminPlainItem{}, listOf("Label", "SupplierID"), "/admin/rel_admin_plain_item/", func(t *testing.T, sqlDB *sql.DB, supplierID int64) {
		insertReturningID(t, sqlDB, `INSERT INTO rel_admin_plain_item (supplier_id, label) VALUES ($1, $2) RETURNING id`, supplierID, "widget")
	})
	related := changelistHeaders(t, relAdminRelatedItem{}, listOf("Label", "SupplierID"), "/admin/rel_admin_related_item/", func(t *testing.T, sqlDB *sql.DB, supplierID int64) {
		insertReturningID(t, sqlDB, `INSERT INTO rel_admin_related_item (supplier_id, label) VALUES ($1, $2) RETURNING id`, supplierID, "widget")
	})
	extra := changelistHeaders(t, relAdminExtraItem{}, listOf("Label", "SupplierID", "Note"), "/admin/rel_admin_extra_item/", func(t *testing.T, sqlDB *sql.DB, supplierID int64) {
		insertReturningID(t, sqlDB, `INSERT INTO rel_admin_extra_item (supplier_id, label, note) VALUES ($1, $2, $3) RETURNING id`, supplierID, "widget", "n")
	})

	if len(plain) == 0 {
		t.Fatal("the plain changelist has no column headers, so the comparison proves nothing")
	}
	if len(extra) <= len(plain) {
		t.Fatalf("known positive failed: the extra-column changelist has %d headers, plain has %d", len(extra), len(plain))
	}
	if len(related) != len(plain) {
		t.Fatalf("changelist headers = %v, want the plain model's %v", related, plain)
	}
	for i := range plain {
		if related[i] != plain[i] {
			t.Fatalf("header %d = %q, want %q", i, related[i], plain[i])
		}
	}
}

func listOf(fields ...string) admin.Options {
	return admin.Options{ListDisplay: fields}
}

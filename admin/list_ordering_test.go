package admin_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func TestListViewOrdersRowsAscendingByOrdering(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Ordering:    []string{"Price"},
	})
	seedProduct(t, sqlDB, "Charlie", 30.00)
	seedProduct(t, sqlDB, "Alpha", 10.00)
	seedProduct(t, sqlDB, "Bravo", 20.00)

	response := doRequest(t, handler, "GET", crudBasePath, nil)
	body := response.Body.String()

	assertOrder(t, body, "Alpha", "Bravo", "Charlie")
}

func TestListViewOrdersRowsDescendingByOrdering(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Ordering:    []string{"-Price"},
	})
	seedProduct(t, sqlDB, "Charlie", 30.00)
	seedProduct(t, sqlDB, "Alpha", 10.00)
	seedProduct(t, sqlDB, "Bravo", 20.00)

	response := doRequest(t, handler, "GET", crudBasePath, nil)
	body := response.Body.String()

	assertOrder(t, body, "Charlie", "Bravo", "Alpha")
}

func TestListViewWithSearchQueryStillHonorsOrdering(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Search:      []string{"Name"},
		Ordering:    []string{"-Price"},
	})
	seedProduct(t, sqlDB, "Widget Charlie", 30.00)
	seedProduct(t, sqlDB, "Widget Alpha", 10.00)
	seedProduct(t, sqlDB, "Widget Bravo", 20.00)

	response := doRequest(t, handler, "GET", crudBasePath+"?q=Widget", nil)
	body := response.Body.String()

	assertOrder(t, body, "Widget Charlie", "Widget Bravo", "Widget Alpha")
}

func TestListViewWithNoOrderingStillRenders(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
	})
	seedProduct(t, sqlDB, "Alpha", 10.00)
	seedProduct(t, sqlDB, "Bravo", 20.00)

	response := doRequest(t, handler, "GET", crudBasePath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "Alpha") || !strings.Contains(body, "Bravo") {
		t.Fatalf("expected both rows to render with no Ordering set:\n%s", body)
	}
}

// assertOrder fails the test unless each name in order appears in body, and
// each one's first occurrence comes strictly after the previous name's.
func assertOrder(t *testing.T, body string, order ...string) {
	t.Helper()

	last := -1
	for _, name := range order {
		idx := strings.Index(body, name)
		if idx == -1 {
			t.Fatalf("expected %q to appear in body:\n%s", name, body)
		}
		if idx <= last {
			t.Fatalf("expected %q to appear after the previous entry (row order wrong):\n%s", name, body)
		}
		last = idx
	}
}

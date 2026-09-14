package admin_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

// TestListViewHonorsOrderingOption covers how the list view's row order (and
// basic renderability) responds to the Ordering option: ascending order,
// descending order, ordering preserved alongside a search query, and
// rendering successfully with no Ordering configured at all.
func TestListViewHonorsOrderingOption(t *testing.T) {
	t.Run("ascending Ordering sorts rows by price low to high", func(t *testing.T) {
		handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
			ListDisplay: []string{"Name", "Price"},
			Ordering:    []string{"Price"},
		})
		seedProduct(t, sqlDB, "Charlie", 30.00)
		seedProduct(t, sqlDB, "Alpha", 10.00)
		seedProduct(t, sqlDB, "Bravo", 20.00)

		response := doRequest(t, handler, "GET", crudBasePath, nil)
		assertOrder(t, response.Body.String(), "Alpha", "Bravo", "Charlie")
	})

	t.Run("descending Ordering (-Price) sorts rows by price high to low", func(t *testing.T) {
		handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
			ListDisplay: []string{"Name", "Price"},
			Ordering:    []string{"-Price"},
		})
		seedProduct(t, sqlDB, "Charlie", 30.00)
		seedProduct(t, sqlDB, "Alpha", 10.00)
		seedProduct(t, sqlDB, "Bravo", 20.00)

		response := doRequest(t, handler, "GET", crudBasePath, nil)
		assertOrder(t, response.Body.String(), "Charlie", "Bravo", "Alpha")
	})

	t.Run("Ordering is still honored when a search query filters rows", func(t *testing.T) {
		handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
			ListDisplay: []string{"Name", "Price"},
			Search:      []string{"Name"},
			Ordering:    []string{"-Price"},
		})
		seedProduct(t, sqlDB, "Widget Charlie", 30.00)
		seedProduct(t, sqlDB, "Widget Alpha", 10.00)
		seedProduct(t, sqlDB, "Widget Bravo", 20.00)

		response := doRequest(t, handler, "GET", crudBasePath+"?q=Widget", nil)
		assertOrder(t, response.Body.String(), "Widget Charlie", "Widget Bravo", "Widget Alpha")
	})

	t.Run("list view still renders all rows with no Ordering configured", func(t *testing.T) {
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
	})
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

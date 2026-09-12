package admin_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func countOccurrences(body, substr string) int {
	return strings.Count(body, substr)
}

func TestListPageIncludesLayoutScriptOnce(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Widget", 1)

	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	body := response.Body.String()
	if count := countOccurrences(body, "cdn.tailwindcss.com"); count != 1 {
		t.Fatalf("Tailwind script occurs %d times, want 1: %s", count, body)
	}
}

func TestFormPageIncludesLayoutScriptOnce(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"new/", nil)
	body := response.Body.String()
	if count := countOccurrences(body, "cdn.tailwindcss.com"); count != 1 {
		t.Fatalf("Tailwind script occurs %d times, want 1: %s", count, body)
	}
}

func TestDeletePageIncludesLayoutScriptOnce(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Widget", 1)

	response := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil)
	body := response.Body.String()
	if count := countOccurrences(body, "cdn.tailwindcss.com"); count != 1 {
		t.Fatalf("Tailwind script occurs %d times, want 1: %s", count, body)
	}
}

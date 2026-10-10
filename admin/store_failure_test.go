package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestEveryCRUDViewAnswersA500WithoutLeakingWhenTheModelsTableIsGone(t *testing.T) {
	tests := []struct {
		name, method, path string
		form               url.Values
	}{
		{"list", http.MethodGet, crudBasePath, nil},
		{"search", http.MethodGet, crudBasePath + "?q=x", nil},
		{"create", http.MethodPost, crudBasePath + "new/", url.Values{"Name": {"a"}, "Price": {"1"}}},
		{"edit form", http.MethodGet, crudBasePath + "1/", nil},
		{"edit", http.MethodPost, crudBasePath + "1/", url.Values{"Name": {"a"}, "Price": {"1"}}},
		{"delete form", http.MethodGet, crudBasePath + "1/delete/", nil},
		{"delete", http.MethodPost, crudBasePath + "1/delete/", url.Values{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, sqlDB := buildProductAdmin(t)
			seedProduct(t, sqlDB, "Widget", 2)
			if _, err := sqlDB.Exec(`DROP TABLE crud_product`); err != nil {
				t.Fatal(err)
			}
			response := doRequest(t, handler, tt.method, tt.path, tt.form)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("%s %s = %d, want 500; body %.200s", tt.method, tt.path, response.Code, response.Body.String())
			}
			body := strings.ToLower(response.Body.String())
			for _, leak := range []string{"no such table", "does not exist", "select ", "crud_product"} {
				if strings.Contains(body, leak) {
					t.Fatalf("the 500 leaked %q: %.200s", leak, response.Body.String())
				}
			}
		})
	}
}

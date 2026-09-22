package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProtectedRoute(t *testing.T) {
	service, err := newJWTService()
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Issue("reader-42", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := buildHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		header string
		status int
		body   string
	}{
		{"missing", "", http.StatusUnauthorized, "{\"error\":\"unauthorized\"}\n"},
		{"invalid", "Bearer invalid", http.StatusUnauthorized, "{\"error\":\"unauthorized\"}\n"},
		{"valid", "Bearer " + token, http.StatusOK, "{\"subject\":\"reader-42\"}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			request.Header.Set("Authorization", test.header)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Body.String() != test.body {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

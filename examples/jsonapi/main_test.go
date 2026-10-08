package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

// TestTheGreetingsAppAnswersJSON is the example's point: one hand-written
// app, installed in main.go, answering a named JSON route.
func TestTheGreetingsAppAnswersJSON(t *testing.T) {
	registry, err := tango.BuildRegistry(appConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/greetings/World/", nil))
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"message":"Hello, World!"}` {
		t.Fatalf("GET /greetings/World/ = %d %q", response.Code, response.Body.String())
	}
}

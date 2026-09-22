package jwt

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
)

func TestExtractors(t *testing.T) {
	tests := []struct {
		name      string
		extractor Extractor
		request   *http.Request
		want      string
		missing   bool
	}{
		{"bearer", BearerToken, requestWithHeader("Bearer abc"), "abc", false},
		{"case insensitive bearer", BearerToken, requestWithHeader("bearer abc"), "abc", false},
		{"missing header", BearerToken, httptest.NewRequest(http.MethodGet, "/", nil), "", true},
		{"wrong scheme", BearerToken, requestWithHeader("Basic abc"), "", true},
		{"empty bearer", BearerToken, requestWithHeader("Bearer"), "", true},
		{"query", QueryToken("token"), httptest.NewRequest(http.MethodGet, "/?token=abc", nil), "abc", false},
		{"missing query", QueryToken("token"), httptest.NewRequest(http.MethodGet, "/", nil), "", true},
		{"empty query name", QueryToken(""), httptest.NewRequest(http.MethodGet, "/?token=abc", nil), "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.extractor(test.request)
			if got != test.want || errors.Is(err, ErrMissingToken) != test.missing {
				t.Fatalf("extract = %q, %v", got, err)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	token, err := service.Issue("user-7", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantCalled bool
		wantClaims bool
	}{
		{"missing passes", "", http.StatusNoContent, true, false},
		{"valid installs claims", "Bearer " + token, http.StatusNoContent, true, true},
		{"invalid rejects", "Bearer invalid", http.StatusUnauthorized, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := service.Middleware(BearerToken)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				called = true
				claims, ok := FromContext(request.Context())
				if ok != test.wantClaims || (ok && claims.Subject != "user-7") {
					t.Fatalf("claims = %+v, %v", claims, ok)
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Authorization", test.header)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || called != test.wantCalled {
				t.Fatalf("status=%d called=%v", response.Code, called)
			}
			if response.Code == http.StatusUnauthorized && response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
				t.Fatalf("body = %q", response.Body.String())
			}
		})
	}
}

func TestMiddlewareAndRequireIntegration(t *testing.T) {
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	token, err := service.Issue("subject", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	app := tango.NewApp("jwt-test", func(registry *tango.Registry) error {
		view := Require(func(ctx *tango.Context) error {
			claims, _ := FromContext(ctx.Context())
			return ctx.JSON(http.StatusOK, map[string]string{"subject": claims.Subject})
		})
		return registry.Routes().Include("/", tango.URLs{tango.Path(http.MethodGet, "/protected", view)}, tango.WithMiddleware(service.Middleware(BearerToken)))
	})
	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{app}})
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
	for _, test := range []struct {
		name   string
		header string
		status int
		body   string
	}{
		{"missing", "", http.StatusUnauthorized, "{\"error\":\"unauthorized\"}\n"},
		{"invalid", "Bearer invalid", http.StatusUnauthorized, "{\"error\":\"unauthorized\"}\n"},
		{"valid", "Bearer " + token, http.StatusOK, "{\"subject\":\"subject\"}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", test.header)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Body.String() != test.body {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestMiddlewareExpiredTokenUsesGenericResponse(t *testing.T) {
	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	service.now = func() time.Time { return fixed }
	expired := signRegistered(t, testSecret, "active", claimsAt(fixed.Add(-time.Hour), fixed.Add(-time.Second)))
	handler := service.Middleware(BearerToken)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("downstream called") }))
	request := requestWithHeader("Bearer " + expired)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Body.String() != "{\"error\":\"unauthorized\"}\n" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %q %q", response.Code, response.Body.String(), response.Header().Get("Content-Type"))
	}
}

func TestMiddlewareExtractionFailureUsesGenericResponse(t *testing.T) {
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	extractErr := errors.New("header parser failed")
	handler := service.Middleware(func(*http.Request) (string, error) {
		return "", extractErr
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("downstream called") }))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusUnauthorized || response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func requestWithHeader(value string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(""))
	request.Header.Set("Authorization", value)
	return request
}

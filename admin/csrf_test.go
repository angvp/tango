package admin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCreatePostWithMissingCSRFTokenIsRejected(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	form := url.Values{"Name": {"Widget"}, "Price": {"9.99"}}
	request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestCreatePostWithWrongCSRFTokenIsRejected(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	form := url.Values{"Name": {"Widget"}, "Price": {"9.99"}, "csrf_token": {"not-the-right-token"}}
	request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestCreatePostWithTokenFromADifferentSessionIsRejected(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	// A well-formed CSRF token, but derived from a session the request
	// doesn't actually carry — must not validate against testSessionToken.
	form := url.Values{
		"Name": {"Widget"}, "Price": {"9.99"},
		"csrf_token": {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestFormPageRendersMatchingCSRFToken(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+"new/", nil)

	if !strings.Contains(response.Body.String(), `name="csrf_token" value="`+testSessionCSRFToken+`"`) {
		t.Fatalf("rendered form does not carry the expected CSRF token:\n%s", response.Body.String())
	}
}

func TestDeletePostWithMissingCSRFTokenIsRejected(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	id := seedProduct(t, sqlDB, "Widget", 9.99)

	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s%d/delete/", crudBasePath, id), nil)
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !productExists(t, sqlDB, id) {
		t.Fatal("product was deleted despite a missing CSRF token")
	}
}

func TestLoginPostWithMissingCSRFCookieIsRejected(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)

	form := url.Values{"username": {"admin"}, "password": {"correct-password"}, "csrf_token": {"whatever"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/login/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

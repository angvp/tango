package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestForbiddenPageShowsNoNavigation covers a signed-in account without staff
// access: the page names no model, because the navigation lists the models.
func TestForbiddenPageShowsNoNavigation(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	if _, err := sqlDB.Exec(`UPDATE admin_user SET is_staff = $1 WHERE username = $2`, false, "admin"); err != nil {
		t.Fatal(err)
	}
	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	assertHTMLErrorPage(t, response, http.StatusForbidden, "You do not have access", "This admin account does not have staff access.")
	body := response.Body.String()
	for _, leak := range []string{`href="/admin/crud_product/`, "crud_product"} {
		if strings.Contains(body, leak) {
			t.Fatalf("access page names a model with %q: %.400s", leak, body)
		}
	}
}

// TestFailureBeforeSignInShowsNoNavigation covers a failure while the session
// is looked up, before the account is known: the page names no model.
func TestFailureBeforeSignInShowsNoNavigation(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	if _, err := sqlDB.Exec(`DROP TABLE admin_session`); err != nil {
		t.Fatal(err)
	}
	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	body := response.Body.String()
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %.300s", response.Code, body)
	}
	if strings.Contains(body, "crud_product") {
		t.Fatalf("failure page names a model: %.400s", body)
	}
}

// TestStaffPagesKeepTheNavigation checks that the navigation is still installed
// for a staff account: a missing row's page links the model's changelist.
func TestStaffPagesKeepTheNavigation(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	response := doRequest(t, handler, http.MethodGet, crudBasePath+"999999/", nil)
	if !strings.Contains(response.Body.String(), `href="/admin/crud_product/"`) {
		t.Fatalf("staff 404 page lacks the navigation: %.400s", response.Body.String())
	}
}

// TestUnhandledMethodsGetTheHTMLMethodNotAllowedPage sends PUT and DELETE to
// each page type through the router. Each answer is the HTML page, and its
// Allow header lists the methods the page handles.
func TestUnhandledMethodsGetTheHTMLMethodNotAllowedPage(t *testing.T) {
	productHandler, productDB := buildProductAdmin(t)
	seedProduct(t, productDB, "Widget", 2)
	messageHandler, _ := buildMessageBookAdmin(t)
	tests := []struct {
		name    string
		handler http.Handler
		path    string
		allow   string
	}{
		{"changelist", productHandler, crudBasePath, "GET"},
		{"create form", productHandler, crudBasePath + "new/", "GET, POST"},
		{"edit form", productHandler, crudBasePath + "1/", "GET, POST"},
		{"delete form", productHandler, crudBasePath + "1/delete/", "GET, POST"},
		{"dashboard", productHandler, "/admin/", "GET"},
		{"login", productHandler, "/admin/login/", "GET, POST"},
		{"theme", productHandler, "/admin/theme/", "POST"},
		{"bulk delete", messageHandler, messageBookBasePath + "delete-selected/", "POST"},
	}
	for _, tt := range tests {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			t.Run(tt.name+" "+method, func(t *testing.T) {
				request := httptest.NewRequest(method, tt.path, nil)
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
				response := httptest.NewRecorder()
				tt.handler.ServeHTTP(response, request)
				assertHTMLErrorPage(t, response, http.StatusMethodNotAllowed, "That action is not available", "This page does not accept that kind of request.")
				if got := response.Header().Get("Allow"); got != tt.allow {
					t.Fatalf("Allow = %q, want %q", got, tt.allow)
				}
			})
		}
	}
}

// TestExpiredFormLinksBackToTheForm covers the next step on an expired form:
// it links back to the page the form was posted to, which shows it again.
func TestExpiredFormLinksBackToTheForm(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(url.Values{"Name": {"Widget"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertHTMLErrorPage(t, response, http.StatusForbidden, "Your form expired", "This form was open too long, or you signed in again in another window.")
	if body := response.Body.String(); !strings.Contains(body, `href="`+crudBasePath+`new/"`) || !strings.Contains(body, "Back to the page") {
		t.Fatalf("expired form lacks the link back to the form: %.400s", body)
	}
}

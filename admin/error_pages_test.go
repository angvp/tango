package admin_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

// errorPageForbidden is the text no server-failure or access page may show:
// Go error text, SQL, the framework's own failure body and the JSON envelope
// the admin used before these pages existed.
var errorPageForbidden = []string{
	"internal error", `{"error"`, "no such table",
	"SELECT", "INSERT", "sql:", "strconv", "invalid syntax", "ParseFloat",
	"constraint", "UNIQUE", "invalid or missing CSRF token",
}

// assertHTMLErrorPage checks the shape every admin error page shares: an HTML
// document in the shared layout, the persistent status region, the literal
// title and sentence, and none of the forbidden text.
func assertHTMLErrorPage(t *testing.T, response *httptest.ResponseRecorder, status int, title, sentence string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body %.300s", response.Code, status, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	body := response.Body.String()
	for _, want := range []string{title, sentence, `role="status"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q: %.400s", want, body)
		}
	}
	for _, leak := range errorPageForbidden {
		if strings.Contains(body, leak) {
			t.Fatalf("body leaks %q: %.400s", leak, body)
		}
	}
}

func TestErrorPagesRenderInTheSharedLayout(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		title    string
		sentence string
		request  func(t *testing.T) *httptest.ResponseRecorder
	}{
		{
			name:     "expired form",
			status:   http.StatusForbidden,
			title:    "Your form expired",
			sentence: "This form was open too long, or you signed in again in another window.",
			request: func(t *testing.T) *httptest.ResponseRecorder {
				handler, _ := buildProductAdmin(t)
				request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", strings.NewReader(url.Values{"Name": {"Widget"}, "Price": {"9.99"}}.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response
			},
		},
		{
			name:     "access denied for a signed-in account without staff access",
			status:   http.StatusForbidden,
			title:    "You do not have access",
			sentence: "This admin account does not have staff access.",
			request: func(t *testing.T) *httptest.ResponseRecorder {
				handler, sqlDB := buildProductAdmin(t)
				if _, err := sqlDB.Exec(`UPDATE admin_user SET is_staff = $1 WHERE username = $2`, false, "admin"); err != nil {
					t.Fatal(err)
				}
				return doRequest(t, handler, http.MethodGet, crudBasePath, nil)
			},
		},
		{
			name:     "missing row",
			status:   http.StatusNotFound,
			title:    "We could not find that page",
			sentence: "It may have been deleted, or the link may be wrong.",
			request: func(t *testing.T) *httptest.ResponseRecorder {
				handler, _ := buildProductAdmin(t)
				return doRequest(t, handler, http.MethodGet, crudBasePath+"999999/", nil)
			},
		},
		{
			name:     "too many sign-in attempts",
			status:   http.StatusTooManyRequests,
			title:    "Too many sign-in attempts",
			sentence: "Too many failed sign-in attempts came from this network.",
			request: func(t *testing.T) *httptest.ResponseRecorder {
				handler, _ := buildLoginTestHandler(t)
				var last *httptest.ResponseRecorder
				for i := 0; i < 10; i++ {
					last = doLoginRequest(t, handler, "admin", "wrong-password")
					if last.Code == http.StatusTooManyRequests {
						break
					}
				}
				return last
			},
		},
		{
			name:     "server failure on save",
			status:   http.StatusInternalServerError,
			title:    "Something went wrong",
			sentence: "Your changes were not saved.",
			request: func(t *testing.T) *httptest.ResponseRecorder {
				handler, sqlDB := buildProductAdmin(t)
				seedProduct(t, sqlDB, "Widget", 2)
				if _, err := sqlDB.Exec(`DROP TABLE crud_product`); err != nil {
					t.Fatal(err)
				}
				return doRequest(t, handler, http.MethodPost, crudBasePath+"1/", url.Values{"Name": {"a"}, "Price": {"1"}})
			},
		},
		{
			name:     "server failure on a page load",
			status:   http.StatusInternalServerError,
			title:    "Something went wrong",
			sentence: "This page could not be loaded.",
			request: func(t *testing.T) *httptest.ResponseRecorder {
				handler, sqlDB := buildProductAdmin(t)
				seedProduct(t, sqlDB, "Widget", 2)
				if _, err := sqlDB.Exec(`DROP TABLE crud_product`); err != nil {
					t.Fatal(err)
				}
				return doRequest(t, handler, http.MethodGet, crudBasePath, nil)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertHTMLErrorPage(t, tt.request(t), tt.status, tt.title, tt.sentence)
		})
	}
}

// TestServerFailureShowsTheRequestReferenceAndLogsTheSameOne checks that the
// page shows the request's reference and nothing technical, and that the log
// line carries the full error with that same reference.
func TestServerFailureShowsTheRequestReferenceAndLogsTheSameOne(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Widget", 2)
	if _, err := sqlDB.Exec(`DROP TABLE crud_product`); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	restore := captureDefaultLog(&logs)
	defer restore()

	withReference := tango.RequestID()(handler)
	request := httptest.NewRequest(http.MethodGet, crudBasePath, nil)
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	withReference.ServeHTTP(response, request)

	reference := response.Header().Get("X-Request-ID")
	if reference == "" {
		t.Fatal("RequestID did not set X-Request-ID")
	}
	if !strings.Contains(response.Body.String(), reference) {
		t.Fatalf("page does not show the reference %q: %.400s", reference, response.Body.String())
	}
	if !strings.Contains(logs.String(), "request_id="+reference) {
		t.Fatalf("log lacks request_id=%s: %s", reference, logs.String())
	}
	if !strings.Contains(logs.String(), "no such table") {
		t.Fatalf("log lacks the full error text: %s", logs.String())
	}
	if strings.Contains(response.Body.String(), "no such table") {
		t.Fatalf("page shows the error text: %.400s", response.Body.String())
	}
}

// TestServerFailureWithoutRequestIDOmitsTheReference covers a host that runs
// no request middleware: the page has no reference label to show.
func TestServerFailureWithoutRequestIDOmitsTheReference(t *testing.T) {
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Widget", 2)
	if _, err := sqlDB.Exec(`DROP TABLE crud_product`); err != nil {
		t.Fatal(err)
	}
	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "Reference") {
		t.Fatalf("page shows a reference label with no reference: %.400s", response.Body.String())
	}
}

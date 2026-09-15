package accounts_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// alwaysErrorReader simulates a request body that can't be read (e.g. a
// client that drops the connection mid-upload), for covering ParseForm's
// error branch on POST views.
type alwaysErrorReader struct{}

func (*alwaysErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

// TestPostWithUnreadableBodyReturnsError covers loginView and registerView's
// shared ParseForm error branch: a request body that fails to read must
// surface as a server error rather than panicking or being silently
// ignored.
func TestPostWithUnreadableBodyReturnsError(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		buildHandler func(t *testing.T) http.Handler
		fetchCSRF    func(t *testing.T, handler http.Handler) *http.Cookie
	}{
		{
			name: "login",
			path: "/accounts/login/",
			buildHandler: func(t *testing.T) http.Handler {
				handler, _ := buildLoginTestHandler(t)
				return handler
			},
			fetchCSRF: fetchLoginCSRF,
		},
		{
			name: "register",
			path: "/accounts/register/",
			buildHandler: func(t *testing.T) http.Handler {
				handler, _ := buildRegisterTestHandler(t)
				return handler
			},
			fetchCSRF: fetchRegisterCSRF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.buildHandler(t)
			csrfCookie := tt.fetchCSRF(t, handler)

			request := httptest.NewRequest(http.MethodPost, tt.path, &alwaysErrorReader{})
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.ContentLength = -1
			request.AddCookie(csrfCookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code < 500 {
				t.Fatalf("status = %d, want a server error for an unreadable request body", response.Code)
			}
		})
	}
}

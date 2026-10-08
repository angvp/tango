package tango

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// buildBodyHandler serves view at POST /upload/ behind global and route
// middleware.
func buildBodyHandler(t *testing.T, view View, global []Middleware, route ...Middleware) http.Handler {
	t.Helper()
	registry, err := BuildRegistry(Config{
		Middleware: global,
		InstalledApps: []App{NewApp("upload", func(r *Registry) error {
			return r.Routes().Include("/", URLs{Path(http.MethodPost, "/upload/", view, Use(route...))})
		})},
	})
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
	return handler
}

// undeclaredBody hides its length, as a chunked request body does.
type undeclaredBody struct{ io.Reader }

func post(handler http.Handler, body io.Reader, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/upload/", body)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if _, hidden := body.(undeclaredBody); hidden {
		request.ContentLength = -1
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertBodyTooLarge(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.Code)
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] != "request body too large" {
		t.Fatalf("body = %q, want {\"error\":\"request body too large\"}", response.Body.String())
	}
}

func TestMaxBodySizeRejectsADeclaredOversizedBodyBeforeTheView(t *testing.T) {
	ran := false
	handler := buildBodyHandler(t, func(ctx *Context) error { ran = true; return nil }, []Middleware{MaxBodySize(8)})
	assertBodyTooLarge(t, post(handler, strings.NewReader(`{"name":"too long"}`), "application/json"))
	if ran {
		t.Fatal("the View ran for a body declared over the limit")
	}
}

func TestMaxBodySizeRejectsAnUndeclaredOversizedBodyAtTheViewErrorBoundary(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		view        View
	}{
		{"Bind", "application/json", `{"name":"far too long for eight bytes"}`, func(ctx *Context) error {
			var v map[string]string
			return ctx.Bind(&v)
		}},
		{"form parse", "application/x-www-form-urlencoded", url.Values{"name": {"far too long for eight bytes"}}.Encode(), func(ctx *Context) error {
			return ctx.Request().ParseForm()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := buildBodyHandler(t, tt.view, []Middleware{MaxBodySize(8)})
			assertBodyTooLarge(t, post(handler, undeclaredBody{strings.NewReader(tt.body)}, tt.contentType))
		})
	}
}

func TestMaxBodySizeLeavesAViewThatHandlesTheErrorInControl(t *testing.T) {
	handler := buildBodyHandler(t, func(ctx *Context) error {
		_, err := io.ReadAll(ctx.Request().Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "keep it short"})
		}
		return err
	}, []Middleware{MaxBodySize(8)})
	response := post(handler, undeclaredBody{strings.NewReader("far too long for eight bytes")}, "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "keep it short") {
		t.Fatalf("status = %d, body = %q; want the View's own 400", response.Code, response.Body.String())
	}
}

func TestMaxBodySizePassesABodyWithinTheLimit(t *testing.T) {
	for _, body := range []string{"12345678", "1234"} {
		handler := buildBodyHandler(t, func(ctx *Context) error {
			got, err := io.ReadAll(ctx.Request().Body)
			if err != nil {
				return err
			}
			return ctx.JSON(http.StatusOK, map[string]string{"got": string(got)})
		}, []Middleware{MaxBodySize(8)})
		response := post(handler, undeclaredBody{strings.NewReader(body)}, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), body) {
			t.Fatalf("body %q: status = %d, body = %q", body, response.Code, response.Body.String())
		}
	}
}

// TestTheMostRestrictiveBodyLimitWins: a route's larger limit can't raise
// the global one, since the global wrapper sees the body first.
func TestTheMostRestrictiveBodyLimitWins(t *testing.T) {
	read := func(ctx *Context) error {
		_, err := io.ReadAll(ctx.Request().Body)
		return err
	}
	body := strings.Repeat("x", 2<<10)
	for name, handler := range map[string]http.Handler{
		"global 1 KiB, route 1 MiB": buildBodyHandler(t, read, []Middleware{MaxBodySize(1 << 10)}, MaxBodySize(1<<20)),
		"global 1 MiB, route 1 KiB": buildBodyHandler(t, read, []Middleware{MaxBodySize(1 << 20)}, MaxBodySize(1<<10)),
	} {
		t.Run(name, func(t *testing.T) {
			assertBodyTooLarge(t, post(handler, strings.NewReader(body), ""))
			assertBodyTooLarge(t, post(handler, undeclaredBody{strings.NewReader(body)}, ""))
		})
	}
}

func TestMaxBodySizeNeedsAPositiveSize(t *testing.T) {
	for _, n := range []int64{0, -1} {
		func() {
			defer func() {
				recovered := recover()
				if recovered == nil || !strings.Contains(recovered.(string), "MaxBodySize") {
					t.Fatalf("MaxBodySize(%d) recovered %v, want a panic naming MaxBodySize", n, recovered)
				}
			}()
			MaxBodySize(n)
		}()
	}
}

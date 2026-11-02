package tango_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

var (
	errLogReadFailed = errors.New("read failed")
	errLogCustom     = errors.New("custom")
)

type logFailingBody struct{}

func (logFailingBody) Read([]byte) (int, error) { return 0, errLogReadFailed }

// logFailingUnmarshaler is a payload whose UnmarshalJSON returns err.
type logFailingUnmarshaler struct{ err error }

func (u *logFailingUnmarshaler) UnmarshalJSON([]byte) error { return u.err }

func bindFailingUnmarshaler(err error) tango.View {
	return func(ctx *tango.Context) error { return ctx.Bind(&logFailingUnmarshaler{err: err}) }
}

func bindPayload(ctx *tango.Context) error {
	var payload struct {
		Name string `json:"name"`
	}
	if err := ctx.Bind(&payload); err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, payload)
}

// A malformed body is the client's error and is not logged as a View error.
// Any other Bind failure is an application failure: answered 500 and logged
// once, with the logged error being that failure.
func TestBindMalformedJSONLogs(t *testing.T) {
	tests := []struct {
		name       string
		body       io.Reader
		view       tango.View
		wantStatus int
		wantLogs   int
		cause      error // the logged error must match with errors.Is
		causeAs    any   // or, when set, with errors.As
	}{
		{name: "truncated object", body: strings.NewReader(`{"name":`), view: bindPayload, wantStatus: http.StatusBadRequest},
		{name: "syntax error", body: strings.NewReader(`{"name":}`), view: bindPayload, wantStatus: http.StatusBadRequest},
		{name: "type mismatch", body: strings.NewReader(`{"name":5}`), view: bindPayload, wantStatus: http.StatusBadRequest},
		{name: "empty body", body: strings.NewReader(``), view: bindPayload, wantStatus: http.StatusBadRequest},
		{name: "custom UnmarshalJSON syntax error", body: strings.NewReader(`{"name":"ok"}`), view: bindFailingUnmarshaler(&json.SyntaxError{}), wantStatus: http.StatusBadRequest},
		{name: "read failure", body: logFailingBody{}, view: bindPayload, wantStatus: http.StatusInternalServerError, wantLogs: 1, cause: errLogReadFailed},
		{name: "custom UnmarshalJSON error", body: strings.NewReader(`{"name":"ok"}`), view: bindFailingUnmarshaler(errLogCustom), wantStatus: http.StatusInternalServerError, wantLogs: 1, cause: errLogCustom},
		{
			name:       "nil destination",
			body:       strings.NewReader(`{"name":"ok"}`),
			view:       func(ctx *tango.Context) error { return ctx.Bind(nil) },
			wantStatus: http.StatusInternalServerError,
			wantLogs:   1,
			causeAs:    new(*json.InvalidUnmarshalError),
		},
		{
			name:       "non-pointer destination",
			body:       strings.NewReader(`{"name":"ok"}`),
			view:       func(ctx *tango.Context) error { var p struct{ Name string }; return ctx.Bind(p) },
			wantStatus: http.StatusInternalServerError,
			wantLogs:   1,
			causeAs:    new(*json.InvalidUnmarshalError),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, logs, _ := observedSite(t, tango.Config{}, tango.URLs{
				tango.Path(http.MethodPost, "/", tt.view),
			}, nil)
			request := httptest.NewRequest(http.MethodPost, "/items/", tt.body)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			errs := logs.byMessage(tango.EventViewError)
			if len(errs) != tt.wantLogs {
				t.Fatalf("View-error logs = %d, want %d: %+v", len(errs), tt.wantLogs, errs)
			}
			if tt.wantLogs == 0 {
				return
			}
			logged, _ := errs[0].attrs["error"].(error)
			if tt.cause != nil && !errors.Is(logged, tt.cause) {
				t.Fatalf("logged error = %v, want errors.Is %v", logged, tt.cause)
			}
			if tt.causeAs != nil && !errors.As(logged, tt.causeAs) {
				t.Fatalf("logged error = %v, want errors.As %T", logged, tt.causeAs)
			}
		})
	}
}

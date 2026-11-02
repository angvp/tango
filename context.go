package tango

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
)

// ErrInvalidBody reports a malformed request body: JSON that does not parse,
// is truncated or empty, or holds a value of the wrong JSON type. A View that
// returns Bind's error unhandled is answered 400 with
// {"error":"invalid request body"}. The decoder's error stays reachable
// through errors.As.
var ErrInvalidBody = errors.New("tango: invalid request body")

// View is the narrow handler contract every route dispatches to.
type View func(*Context) error

// Context wraps an HTTP request/response pair with high-value helpers.
// Escape hatches (Request, ResponseWriter) are available when the helpers
// below aren't enough.
type Context struct {
	request *http.Request
	writer  http.ResponseWriter
	params  map[string]string
	logger  *slog.Logger
}

func newContextWithLogger(w http.ResponseWriter, r *http.Request, params map[string]string, logger *slog.Logger) *Context {
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []any{slog.String("route", routeIdentity(r)), slog.String("method", r.Method)}
	if requestID, ok := RequestIDFromContext(r.Context()); ok {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	return &Context{
		request: r,
		writer:  w,
		params:  params,
		logger:  logger.With(attrs...),
	}
}

// Logger returns the request-scoped structured logger.
func (c *Context) Logger() *slog.Logger { return c.logger }

// Param returns a named path parameter's value, or "" if absent.
func (c *Context) Param(name string) string {
	return c.params[name]
}

// Query returns a named URL query parameter's value, or "" if absent.
func (c *Context) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

// Bind decodes the request body as JSON into dst. A malformed body matches
// ErrInvalidBody: a JSON syntax or type error, or an end-of-input error
// (io.EOF or io.ErrUnexpectedEOF), anywhere in the error chain, whether it
// comes from the decoder, a custom UnmarshalJSON, or the body reader when a
// client disconnects mid-body. Any other failure, including a body over a
// MaxBodySize limit, a read error other than end-of-input, an invalid dst, or
// any other error from a custom UnmarshalJSON, is returned unwrapped.
func (c *Context) Bind(dst any) error {
	err := json.NewDecoder(c.request.Body).Decode(dst)
	if !isMalformedBody(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrInvalidBody, err)
}

// isMalformedBody whitelists the decoder failures caused by the client's
// bytes. Everything else is an application or transport failure, so a
// rejected dst or a broken reader stays a 500 and is logged.
func isMalformedBody(err error) bool {
	var syntax *json.SyntaxError
	var typeMismatch *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &typeMismatch) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// JSON writes payload as a JSON response body with the given status code.
func (c *Context) JSON(status int, payload any) error {
	c.writer.Header().Set("Content-Type", "application/json")
	c.writer.WriteHeader(status)
	return json.NewEncoder(c.writer).Encode(payload)
}

// HTML renders the named template within tmpl (via tmpl.ExecuteTemplate)
// against data, writing it as an HTML response with the given status code.
// The render is buffered: nothing is written to the response until
// execution fully succeeds, so a template error is returned like any other
// view error (a clean framework-generated 500) instead of a response that
// already committed status and headers followed by a truncated or
// malformed body. tmpl must already be parsed; HTML has no opinion on
// template parsing, caching, or file layout.
func (c *Context) HTML(status int, tmpl *template.Template, name string, data any) error {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return err
	}

	c.writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.writer.WriteHeader(status)
	_, err := buf.WriteTo(c.writer)
	return err
}

// Redirect writes an HTTP redirect response to url.
func (c *Context) Redirect(url string) error {
	http.Redirect(c.writer, c.request, url, http.StatusFound)
	return nil
}

// Request returns the wrapped *http.Request.
func (c *Context) Request() *http.Request {
	return c.request
}

// ResponseWriter returns the wrapped http.ResponseWriter.
func (c *Context) ResponseWriter() http.ResponseWriter {
	return c.writer
}

// Context returns the wrapped request's standard context.Context. Context
// does not itself implement context.Context.
func (c *Context) Context() context.Context {
	return c.request.Context()
}

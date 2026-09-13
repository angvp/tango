package tango

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net/http"
)

// View is the narrow handler contract every route dispatches to.
type View func(*Context) error

// Context wraps an HTTP request/response pair with high-value helpers.
// Escape hatches (Request, ResponseWriter) are available when the helpers
// below aren't enough.
type Context struct {
	request *http.Request
	writer  http.ResponseWriter
	params  map[string]string
}

// newContext builds a Context. Param wiring from the compiled route tree
// lands in a later milestone ticket; params may be nil or supplied directly
// for now.
func newContext(w http.ResponseWriter, r *http.Request, params map[string]string) *Context {
	return &Context{
		request: r,
		writer:  w,
		params:  params,
	}
}

// Param returns a named path parameter's value, or "" if absent.
func (c *Context) Param(name string) string {
	return c.params[name]
}

// Query returns a named URL query parameter's value, or "" if absent.
func (c *Context) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

// Bind decodes the request body as JSON into dst.
func (c *Context) Bind(dst any) error {
	return json.NewDecoder(c.request.Body).Decode(dst)
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
// malformed body — see ADR 0015. tmpl must already be parsed; HTML has no
// opinion on template parsing, caching, or file layout.
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

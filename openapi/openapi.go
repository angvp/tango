package openapi

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/angvp/tango"
)

// Options configures New and Document. The zero value is usable.
type Options struct {
	// Title is the API's name. Default "API".
	Title string
	// Version is the API's own version, not the OpenAPI version. Default "0.0.0".
	Version string
	// Description is shown under the title.
	Description string
	// Servers are base URLs such as "https://api.example.com".
	Servers []string
	// Path is where the JSON document is served. Default "/openapi.json".
	Path string
	// ExcludePrefixes leaves out every route whose path starts with one of
	// them, such as "/admin/" for pages that are not part of the API.
	ExcludePrefixes []string
}

func (o Options) withDefaults() Options {
	if o.Title == "" {
		o.Title = "API"
	}
	if o.Version == "" {
		o.Version = "0.0.0"
	}
	if o.Path == "" {
		o.Path = "/openapi.json"
	}
	return o
}

// New returns an App that serves the OpenAPI document of every route in the
// registry at Options.Path. Nothing is served unless it is installed; wrap the
// route with your own middleware if the document should not be public.
func New(opts Options) tango.App {
	opts = opts.withDefaults()
	return tango.NewApp("openapi", func(registry *tango.Registry) error {
		docs := &served{registry: registry, opts: opts}
		return registry.Routes().Include("", []tango.Route{
			tango.Path(http.MethodGet, opts.Path, docs.serveJSON,
				tango.Name("openapi.json"), tango.Doc(tango.RouteDoc{Hidden: true})),
		})
	})
}

// served builds the document on first use: routes are fixed once the server
// is running, so one build serves every request.
type served struct {
	registry *tango.Registry
	opts     Options

	once sync.Once
	body []byte
	err  error
}

func (s *served) document() ([]byte, error) {
	s.once.Do(func() {
		doc, err := Document(s.registry.Routes().Described(), s.opts)
		if err != nil {
			s.err = err
			return
		}
		s.body, s.err = json.MarshalIndent(doc, "", "  ")
	})
	return s.body, s.err
}

func (s *served) serveJSON(ctx *tango.Context) error {
	body, err := s.document()
	if err != nil {
		return err
	}
	w := ctx.ResponseWriter()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, err = w.Write(append(body, '\n'))
	return err
}

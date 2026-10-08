package tango_test

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

// viewsSite serves GET /items/{id}/ and GET+POST /items/, with config's
// unmatched Views and scope, observed by a capturing logger and recorder.
func viewsSite(t *testing.T, config tango.Config, global func(*slog.Logger) []tango.Middleware) (http.Handler, *scopeLogs, *scopeMetrics) {
	t.Helper()
	logger, logs := newScopeLogger()
	metrics := &scopeMetrics{}
	ok := func(ctx *tango.Context) error { return ctx.JSON(http.StatusOK, nil) }
	config.InstalledApps = []tango.App{tango.NewApp("items", func(r *tango.Registry) error {
		return r.Routes().Include("/items/", tango.URLs{
			tango.Path(http.MethodGet, "/{id}/", ok),
			tango.Path(http.MethodGet, "/", ok),
			tango.Path(http.MethodPost, "/", ok),
		})
	})}
	if global != nil {
		config.Middleware = global(logger)
	}
	registry, err := tango.BuildRegistry(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	tango.ObserveRoutes(registry, logger, metrics)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, logs, metrics
}

var scopes = map[string]tango.MiddlewareScope{"Routes": tango.MiddlewareScopeRoutes, "All": tango.MiddlewareScopeAll}

func notFoundPage(ctx *tango.Context) error {
	return ctx.JSON(http.StatusNotFound, map[string]string{"error": "no such page"})
}

func methodPage(ctx *tango.Context) error {
	return ctx.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "wrong method"})
}

func TestUnsetUnmatchedViewsKeepTheRouterResponses(t *testing.T) {
	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			handler, _, _ := viewsSite(t, tango.Config{MiddlewareScope: scope}, nil)
			notFound := serve(handler, http.MethodGet, "/nowhere/", "")
			if notFound.Code != 404 || notFound.Body.String() != "404 page not found\n" ||
				notFound.Header().Get("Content-Type") != "text/plain; charset=utf-8" || notFound.Header().Get("X-Content-Type-Options") != "nosniff" || len(notFound.Header()) != 2 {
				t.Fatalf("404 = %d %v %q, want the router's own", notFound.Code, notFound.Header(), notFound.Body.String())
			}
			methodNot := serve(handler, http.MethodDelete, "/items/42/", "")
			if methodNot.Code != 405 || methodNot.Body.Len() != 0 || len(methodNot.Header()) != 1 || methodNot.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("405 = %d %v %q, want the router's own", methodNot.Code, methodNot.Header(), methodNot.Body.String())
			}
		})
	}
}

func TestCustomUnmatchedViewsAnswerUnderBothScopes(t *testing.T) {
	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			handler, _, _ := viewsSite(t, tango.Config{MiddlewareScope: scope, NotFound: notFoundPage, MethodNotAllowed: methodPage}, nil)
			if r := serve(handler, http.MethodGet, "/nowhere/", ""); r.Code != 404 || !strings.Contains(r.Body.String(), "no such page") {
				t.Fatalf("404 = %d %q, want the custom View", r.Code, r.Body.String())
			}
			tests := []struct {
				target string
				allow  []string
			}{
				{"/items/42/", []string{http.MethodGet}},
				{"/items/", []string{http.MethodGet, http.MethodPost}},
			}
			for _, tt := range tests {
				r := serve(handler, http.MethodDelete, tt.target, "")
				if r.Code != 405 || !strings.Contains(r.Body.String(), "wrong method") {
					t.Fatalf("DELETE %s = %d %q, want the custom View", tt.target, r.Code, r.Body.String())
				}
				allow := r.Header().Values("Allow")
				slices.Sort(allow)
				if !slices.Equal(allow, tt.allow) {
					t.Fatalf("DELETE %s Allow = %v, want %v", tt.target, allow, tt.allow)
				}
			}
		})
	}
}

func TestAFailingUnmatchedViewTakesTheViewErrorPath(t *testing.T) {
	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			failing := func(*tango.Context) error { return errors.New("boom") }
			handler, logs, _ := viewsSite(t, tango.Config{MiddlewareScope: scope, NotFound: failing}, func(*slog.Logger) []tango.Middleware { return nil })
			r := serve(handler, http.MethodGet, "/nowhere/", "")
			if r.Code != 500 || strings.TrimSpace(r.Body.String()) != `{"error":"internal error"}` {
				t.Fatalf("= %d %q, want the generic 500", r.Code, r.Body.String())
			}
			if errs := logs.byMessage(tango.EventViewError); len(errs) != 1 || errs[0].attrs["route"] != "(unmatched)" {
				t.Fatalf("View-error logs = %+v, want one with route (unmatched)", errs)
			}
		})
	}
}

func TestCustomUnmatchedViewsAreObservedOnlyUnderScopeAll(t *testing.T) {
	var order []string
	mark := func(name string) tango.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	view := func(ctx *tango.Context) error {
		order = append(order, "view")
		return notFoundPage(ctx)
	}
	tests := []struct {
		scope     tango.MiddlewareScope
		wantOrder []string
		observed  bool
	}{
		{tango.MiddlewareScopeAll, []string{"outer", "inner", "view"}, true},
		{tango.MiddlewareScopeRoutes, []string{"view"}, false},
	}
	for _, tt := range tests {
		order = nil
		handler, logs, metrics := viewsSite(t, tango.Config{MiddlewareScope: tt.scope, NotFound: view}, func(l *slog.Logger) []tango.Middleware {
			return []tango.Middleware{mark("outer"), tango.AccessLogger(tango.WithAccessLogger(l)), mark("inner")}
		})
		serve(handler, http.MethodGet, "/nowhere/", "")
		if !slices.Equal(order, tt.wantOrder) {
			t.Fatalf("scope %d: order = %v, want %v", tt.scope, order, tt.wantOrder)
		}
		access := logs.byMessage(tango.EventAccessLog)
		observed := len(access) == 1 && access[0].attrs["route"] == "(unmatched)" && len(metrics.observations) == 1 && metrics.observations[0]["route"] == "(unmatched)"
		if observed != tt.observed || (!tt.observed && (len(access) != 0 || len(metrics.observations) != 0)) {
			t.Fatalf("scope %d: access %+v, metrics %+v; want observed=%v", tt.scope, access, metrics.observations, tt.observed)
		}
	}
}

package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/angvp/tango"
)

func TestTFallbackAndLookupBehavior(t *testing.T) {
	cases := []struct {
		name     string
		catalogs map[string]map[string]string
		locale   string
		key      string
		fallback string
		args     []any
		want     string
	}{
		{
			name:     "falls back when no locale is set on context",
			key:      "admin.login.title",
			fallback: "Log in",
			want:     "Log in",
		},
		{
			name:     "falls back when no catalog exists for the set locale",
			locale:   "es",
			key:      "admin.login.title",
			fallback: "Log in",
			want:     "Log in",
		},
		{
			name:     "uses catalog value when key exists in registered locale",
			catalogs: map[string]map[string]string{"es": {"admin.login.title": "Iniciar sesión"}},
			locale:   "es",
			key:      "admin.login.title",
			fallback: "Log in",
			want:     "Iniciar sesión",
		},
		{
			name:     "falls back when catalog exists but key is missing",
			catalogs: map[string]map[string]string{"es": {"admin.login.title": "Iniciar sesión"}},
			locale:   "es",
			key:      "admin.logout.title",
			fallback: "Log out",
			want:     "Log out",
		},
		{
			name:     "formats catalog value when args are present",
			catalogs: map[string]map[string]string{"es": {"admin.create.title": "Nuevo %s"}},
			locale:   "es",
			key:      "admin.create.title",
			fallback: "New %s",
			args:     []any{"Post"},
			want:     "Nuevo Post",
		},
		{
			name:     "formats fallback when args are present and catalog value is absent",
			locale:   "es",
			key:      "admin.create.title",
			fallback: "New %s",
			args:     []any{"Post"},
			want:     "New Post",
		},
		{
			name:     "does not format catalog value when args are absent",
			catalogs: map[string]map[string]string{"es": {"admin.percent": "Progress: %s"}},
			locale:   "es",
			key:      "admin.percent",
			fallback: "Progress: %s",
			want:     "Progress: %s",
		},
		{
			name: "tries exact locale before base language",
			catalogs: map[string]map[string]string{
				"es":    {"admin.login.title": "Iniciar sesión"},
				"es-MX": {"admin.login.title": "Entrar"},
			},
			locale:   "es-MX",
			key:      "admin.login.title",
			fallback: "Log in",
			want:     "Entrar",
		},
		{
			name:     "falls back to base language catalog when exact locale is missing",
			catalogs: map[string]map[string]string{"es": {"admin.login.title": "Iniciar sesión"}},
			locale:   "es-MX",
			key:      "admin.login.title",
			fallback: "Log in",
			want:     "Iniciar sesión",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCatalogsForTest()
			for locale, catalog := range tc.catalogs {
				RegisterCatalog(locale, catalog)
			}

			ctx := context.Background()
			if tc.locale != "" {
				ctx = WithLocale(ctx, tc.locale)
			}

			got := T(ctx, tc.key, tc.fallback, tc.args...)

			if got != tc.want {
				t.Errorf("case %q: T returned %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRegisterCatalogMergeSemantics(t *testing.T) {
	cases := []struct {
		name          string
		registrations []struct {
			locale  string
			catalog map[string]string
		}
		checks []struct {
			locale   string
			key      string
			fallback string
			want     string
		}
	}{
		{
			name: "merges keys registered separately for the same locale",
			registrations: []struct {
				locale  string
				catalog map[string]string
			}{
				{"es", map[string]string{"admin.login.title": "Iniciar sesión"}},
				{"es", map[string]string{"admin.button.save": "Guardar"}},
			},
			checks: []struct {
				locale   string
				key      string
				fallback string
				want     string
			}{
				{"es", "admin.login.title", "Log in", "Iniciar sesión"},
				{"es", "admin.button.save", "Save", "Guardar"},
			},
		},
		{
			name: "last write wins per key for the same locale",
			registrations: []struct {
				locale  string
				catalog map[string]string
			}{
				{"es", map[string]string{"admin.button.save": "Salvar"}},
				{"es", map[string]string{"admin.button.save": "Guardar"}},
			},
			checks: []struct {
				locale   string
				key      string
				fallback string
				want     string
			}{
				{"es", "admin.button.save", "Save", "Guardar"},
			},
		},
		{
			name: "keeps locales isolated from one another",
			registrations: []struct {
				locale  string
				catalog map[string]string
			}{
				{"es", map[string]string{"admin.button.save": "Guardar"}},
				{"fr", map[string]string{"admin.button.save": "Enregistrer"}},
			},
			checks: []struct {
				locale   string
				key      string
				fallback string
				want     string
			}{
				{"es", "admin.button.save", "Save", "Guardar"},
				{"fr", "admin.button.save", "Save", "Enregistrer"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCatalogsForTest()
			for _, reg := range tc.registrations {
				RegisterCatalog(reg.locale, reg.catalog)
			}

			for _, check := range tc.checks {
				got := T(WithLocale(context.Background(), check.locale), check.key, check.fallback)
				if got != check.want {
					t.Errorf("case %q: T(locale=%q, key=%q) = %q, want %q", tc.name, check.locale, check.key, got, check.want)
				}
			}
		})
	}
}

func TestDefaultLocaleResolverResolution(t *testing.T) {
	cases := []struct {
		name           string
		catalogLocale  string
		catalog        map[string]string
		acceptHeaders  []string
		wantForHeaders string
	}{
		{
			name:           "matches exact locale over a higher-quality base language",
			catalogLocale:  "es-MX",
			catalog:        map[string]string{"hello": "qué onda"},
			acceptHeaders:  []string{"fr;q=0.8, es-MX;q=0.9, es;q=0.7"},
			wantForHeaders: "es-MX",
		},
		{
			name:           "matches base language when exact locale catalog is missing",
			catalogLocale:  "es",
			catalog:        map[string]string{"hello": "hola"},
			acceptHeaders:  []string{"es-MX, fr;q=0.9"},
			wantForHeaders: "es",
		},
		{
			name:           "falls through to empty locale for missing or malformed header",
			catalogLocale:  "es",
			catalog:        map[string]string{"hello": "hola"},
			acceptHeaders:  []string{"", "::::", "es;q=not-a-number"},
			wantForHeaders: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCatalogsForTest()
			RegisterCatalog(tc.catalogLocale, tc.catalog)

			for _, header := range tc.acceptHeaders {
				request := httptest.NewRequest(http.MethodGet, "/", nil)
				request.Header.Set("Accept-Language", header)

				if got := DefaultLocaleResolver(request); got != tc.wantForHeaders {
					t.Errorf("case %q: DefaultLocaleResolver(%q) returned %q, want %q", tc.name, header, got, tc.wantForHeaders)
				}
			}
		})
	}
}

func TestMiddlewareThreadsResolvedLocaleIntoView(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"greeting": "hola"})
	app := tango.NewApp("test", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/hello/", func(ctx *tango.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{
					"message": T(ctx.Context(), "greeting", "hello"),
				})
			}),
		})
	})
	registry, err := tango.BuildRegistry(tango.Config{
		InstalledApps: []tango.App{app},
		Middleware:    []tango.Middleware{Middleware(nil)},
	})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/hello/", nil)
	request.Header.Set("Accept-Language", "es-MX")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Body.String(); got != "{\"message\":\"hola\"}\n" {
		t.Fatalf("response body = %q, want translated message", got)
	}
}

func TestWithoutMiddlewareTStillFallsBackInsideView(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"greeting": "hola"})
	app := tango.NewApp("test", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/hello/", func(ctx *tango.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{
					"message": T(ctx.Context(), "greeting", "hello"),
				})
			}),
		})
	})
	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{app}})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/hello/", nil)
	request.Header.Set("Accept-Language", "es")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Body.String(); got != "{\"message\":\"hello\"}\n" {
		t.Fatalf("response body = %q, want fallback message", got)
	}
}

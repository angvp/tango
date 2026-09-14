package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/angvp/tango"
)

func TestTFallsBackWhenNoLocaleIsSet(t *testing.T) {
	resetCatalogsForTest()

	got := T(context.Background(), "admin.login.title", "Log in")

	if got != "Log in" {
		t.Fatalf("T returned %q, want fallback", got)
	}
}

func TestTFallsBackWhenNoCatalogExistsForLocale(t *testing.T) {
	resetCatalogsForTest()
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.login.title", "Log in")

	if got != "Log in" {
		t.Fatalf("T returned %q, want fallback", got)
	}
}

func TestTUsesCatalogValueWhenKeyExists(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.login.title": "Iniciar sesión"})
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.login.title", "Log in")

	if got != "Iniciar sesión" {
		t.Fatalf("T returned %q, want catalog value", got)
	}
}

func TestTFallsBackWhenCatalogKeyIsMissing(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.login.title": "Iniciar sesión"})
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.logout.title", "Log out")

	if got != "Log out" {
		t.Fatalf("T returned %q, want fallback", got)
	}
}

func TestTFormatsCatalogValueWhenArgsArePresent(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.create.title": "Nuevo %s"})
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.create.title", "New %s", "Post")

	if got != "Nuevo Post" {
		t.Fatalf("T returned %q, want formatted catalog value", got)
	}
}

func TestTFormatsFallbackWhenArgsArePresent(t *testing.T) {
	resetCatalogsForTest()
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.create.title", "New %s", "Post")

	if got != "New Post" {
		t.Fatalf("T returned %q, want formatted fallback", got)
	}
}

func TestTDoesNotFormatWhenArgsAreAbsent(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.percent": "Progress: %s"})
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.percent", "Progress: %s")

	if got != "Progress: %s" {
		t.Fatalf("T returned %q, want unformatted catalog value", got)
	}
}

func TestTTriesExactLocaleBeforeBaseLanguage(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.login.title": "Iniciar sesión"})
	RegisterCatalog("es-MX", map[string]string{"admin.login.title": "Entrar"})
	ctx := WithLocale(context.Background(), "es-MX")

	got := T(ctx, "admin.login.title", "Log in")

	if got != "Entrar" {
		t.Fatalf("T returned %q, want exact-locale catalog value", got)
	}
}

func TestTFallsBackToBaseLanguageCatalog(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.login.title": "Iniciar sesión"})
	ctx := WithLocale(context.Background(), "es-MX")

	got := T(ctx, "admin.login.title", "Log in")

	if got != "Iniciar sesión" {
		t.Fatalf("T returned %q, want base-language catalog value", got)
	}
}

func TestRegisterCatalogMergesKeysForSameLocale(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.login.title": "Iniciar sesión"})
	RegisterCatalog("es", map[string]string{"admin.button.save": "Guardar"})
	ctx := WithLocale(context.Background(), "es")

	login := T(ctx, "admin.login.title", "Log in")
	save := T(ctx, "admin.button.save", "Save")

	if login != "Iniciar sesión" || save != "Guardar" {
		t.Fatalf("merged catalog values = %q, %q; want both keys preserved", login, save)
	}
}

func TestRegisterCatalogLastWriteWinsPerKey(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.button.save": "Salvar"})
	RegisterCatalog("es", map[string]string{"admin.button.save": "Guardar"})
	ctx := WithLocale(context.Background(), "es")

	got := T(ctx, "admin.button.save", "Save")

	if got != "Guardar" {
		t.Fatalf("T returned %q, want last registered value", got)
	}
}

func TestRegisterCatalogKeepsLocalesIsolated(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"admin.button.save": "Guardar"})
	RegisterCatalog("fr", map[string]string{"admin.button.save": "Enregistrer"})

	spanish := T(WithLocale(context.Background(), "es"), "admin.button.save", "Save")
	french := T(WithLocale(context.Background(), "fr"), "admin.button.save", "Save")

	if spanish != "Guardar" || french != "Enregistrer" {
		t.Fatalf("catalog values = %q, %q; want locales isolated", spanish, french)
	}
}

func TestDefaultLocaleResolverMatchesExactLocale(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es-MX", map[string]string{"hello": "qué onda"})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Language", "fr;q=0.8, es-MX;q=0.9, es;q=0.7")

	got := DefaultLocaleResolver(request)

	if got != "es-MX" {
		t.Fatalf("DefaultLocaleResolver returned %q, want exact locale", got)
	}
}

func TestDefaultLocaleResolverMatchesBaseLanguage(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"hello": "hola"})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Language", "es-MX, fr;q=0.9")

	got := DefaultLocaleResolver(request)

	if got != "es" {
		t.Fatalf("DefaultLocaleResolver returned %q, want base language", got)
	}
}

func TestDefaultLocaleResolverFallsThroughForMissingOrMalformedHeader(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"hello": "hola"})

	for _, header := range []string{"", "::::", "es;q=not-a-number"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Language", header)

		if got := DefaultLocaleResolver(request); got != "" {
			t.Fatalf("DefaultLocaleResolver(%q) returned %q, want empty locale", header, got)
		}
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

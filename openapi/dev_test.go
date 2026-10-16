package openapi_test

import (
	"net/http"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
	"github.com/angvp/tango/openapi/swaggerui"
)

func devHandler(t *testing.T) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	for _, app := range []tango.App{
		tango.NewApp("shop", shopRoutes),
		openapi.Dev(openapi.Options{UI: swaggerui.New()}),
	} {
		if err := registry.Register(app); err != nil {
			t.Fatal(err)
		}
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

func TestDevServesNothingUnlessTheVariableIsTrue(t *testing.T) {
	for _, value := range []string{"", "0", "false", "nonsense"} {
		t.Setenv(openapi.EnvVar, value)
		h := devHandler(t)
		for _, path := range []string{"/openapi.json", "/docs/"} {
			if rec := fetch(t, h, path); rec.Code != http.StatusNotFound {
				t.Errorf("%s=%q: GET %s = %d, want 404", openapi.EnvVar, value, path, rec.Code)
			}
		}
	}
}

func TestDevServesTheDocumentAndUIWhenTheVariableIsTrue(t *testing.T) {
	for _, value := range []string{"1", "true"} {
		t.Setenv(openapi.EnvVar, value)
		h := devHandler(t)
		for _, path := range []string{"/openapi.json", "/docs/"} {
			if rec := fetch(t, h, path); rec.Code != http.StatusOK {
				t.Errorf("%s=%q: GET %s = %d, want 200", openapi.EnvVar, value, path, rec.Code)
			}
		}
	}
}

func TestDevStillFeedsTheCLIAndCheckWhenOff(t *testing.T) {
	t.Setenv(openapi.EnvVar, "")
	body, err := tango.DumpOpenAPI(tango.Config{InstalledApps: []tango.App{
		tango.NewApp("shop", shopRoutes), openapi.Dev(openapi.Options{Title: "Shop"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	checkDocument(t, body)
}

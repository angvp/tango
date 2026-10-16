package openapi_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
)

func TestDumpOpenAPIIsByteStableAndNeedsTheApp(t *testing.T) {
	config := tango.Config{InstalledApps: []tango.App{tango.NewApp("shop", shopRoutes), openapi.New(openapi.Options{Title: "Shop"})}}
	first, err := tango.DumpOpenAPI(config)
	if err != nil {
		t.Fatal(err)
	}
	config = tango.Config{InstalledApps: []tango.App{tango.NewApp("shop", shopRoutes), openapi.New(openapi.Options{Title: "Shop"})}}
	second, err := tango.DumpOpenAPI(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !bytes.HasSuffix(first, []byte("}\n")) {
		t.Fatalf("output is not stable or not newline-terminated:\n%s\n%s", first, second)
	}
	checkDocument(t, first)

	_, err = tango.DumpOpenAPI(tango.Config{InstalledApps: []tango.App{tango.NewApp("shop", shopRoutes)}})
	if !errors.Is(err, tango.ErrNoOpenAPI) {
		t.Fatalf("err = %v, want ErrNoOpenAPI", err)
	}
}

package tango_test

import (
	"errors"
	"testing"

	"github.com/angvp/tango"
)

func noopView(ctx *tango.Context) error { return nil }

func TestDescribedListsRoutesInOrderAsCopies(t *testing.T) {
	registry := tango.NewRegistry()
	app := tango.NewApp("shop", func(r *tango.Registry) error {
		return r.Routes().Include("/pets", []tango.Route{
			tango.Path("GET", "/{id}", noopView, tango.Name("detail"),
				tango.Doc(tango.RouteDoc{Summary: "One pet", Tags: []string{"pets"}})),
			tango.Path("POST", "/", noopView),
		})
	})
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}

	got := registry.Routes().Described()
	if len(got) != 2 {
		t.Fatalf("described %d routes, want 2: %+v", len(got), got)
	}
	if got[0].Method != "GET" || got[0].Pattern != "/pets/{id}" || got[0].Name != "pets:detail" {
		t.Fatalf("first route = %+v", got[0])
	}
	if got[0].Prefix != "pets" {
		t.Errorf("Prefix = %q, want pets", got[0].Prefix)
	}
	if got[0].Doc == nil || got[0].Doc.Summary != "One pet" {
		t.Fatalf("Doc = %+v", got[0].Doc)
	}
	if got[1].Method != "POST" || got[1].Doc != nil {
		t.Errorf("second route = %+v, want POST without a Doc", got[1])
	}

	got[0].Doc.Tags[0] = "changed"
	got[0].Doc.Summary = "changed"
	again := registry.Routes().Described()
	if again[0].Doc.Tags[0] != "pets" || again[0].Doc.Summary != "One pet" {
		t.Errorf("changing a returned value changed the registry: %+v", again[0].Doc)
	}
}

type fakeOpenAPI struct {
	tango.App
	body []byte
	err  error
}

func (f fakeOpenAPI) OpenAPI() ([]byte, error) { return f.body, f.err }

func TestDumpOpenAPIUsesTheFirstProvider(t *testing.T) {
	plain := tango.NewApp("plain", func(r *tango.Registry) error { return nil })
	provider := fakeOpenAPI{App: tango.NewApp("docs", func(r *tango.Registry) error { return nil }), body: []byte("{}\n")}

	body, err := tango.DumpOpenAPI(tango.Config{InstalledApps: []tango.App{plain, provider}})
	if err != nil || string(body) != "{}\n" {
		t.Fatalf("DumpOpenAPI = %q, %v", body, err)
	}

	_, err = tango.DumpOpenAPI(tango.Config{InstalledApps: []tango.App{plain}})
	if !errors.Is(err, tango.ErrNoOpenAPI) {
		t.Errorf("err = %v, want ErrNoOpenAPI", err)
	}

	boom := errors.New("cannot describe")
	provider.err = boom
	if _, err = tango.DumpOpenAPI(tango.Config{InstalledApps: []tango.App{provider}}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the provider's error", err)
	}

	failing := tango.NewApp("failing", func(r *tango.Registry) error { return errors.New("registration failed") })
	if _, err = tango.DumpOpenAPI(tango.Config{InstalledApps: []tango.App{failing, provider}}); err == nil {
		t.Error("a registration error must stop DumpOpenAPI")
	}
}

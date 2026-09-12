package tango

import "testing"

type appOwnedWidget struct {
	ID int64 `tango:"pk"`
}

type appOwnedGadget struct {
	ID int64 `tango:"pk"`
}

func TestRunRegistrationAttributesModelsToRegisteringApp(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(NewApp("widgets", func(r *Registry) error {
		return r.Models().Register(appOwnedWidget{})
	})); err != nil {
		t.Fatalf("Register(widgets) returned error: %v", err)
	}

	if err := registry.Register(NewApp("gadgets", func(r *Registry) error {
		return r.Models().Register(appOwnedGadget{})
	})); err != nil {
		t.Fatalf("Register(gadgets) returned error: %v", err)
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	widgetMeta, ok := registry.Models().Get("appOwnedWidget")
	if !ok {
		t.Fatalf("model appOwnedWidget not registered")
	}
	if widgetMeta.App != "widgets" {
		t.Fatalf("appOwnedWidget.App = %q, want %q", widgetMeta.App, "widgets")
	}

	gadgetMeta, ok := registry.Models().Get("appOwnedGadget")
	if !ok {
		t.Fatalf("model appOwnedGadget not registered")
	}
	if gadgetMeta.App != "gadgets" {
		t.Fatalf("appOwnedGadget.App = %q, want %q", gadgetMeta.App, "gadgets")
	}
}

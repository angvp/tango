package tango

import "testing"

func TestReverserReverseBuildsURLFromParams(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/{id}", noopView, Name("detail"))}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	rv, err := routes.Reverser()
	if err != nil {
		t.Fatalf("Reverser() returned error: %v", err)
	}

	url, err := rv.Reverse("users:detail", Params{"id": "42"})
	if err != nil {
		t.Fatalf("Reverse returned error: %v", err)
	}
	if want := "/users/42"; url != want {
		t.Fatalf("Reverse = %q, want %q", url, want)
	}
}

func TestReverserReverseUnknownNameFails(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/{id}", noopView, Name("detail"))}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	rv, err := routes.Reverser()
	if err != nil {
		t.Fatalf("Reverser() returned error: %v", err)
	}

	if _, err := rv.Reverse("users:missing", Params{}); err == nil {
		t.Fatal("Reverse returned nil error for unknown name, want non-nil")
	}
}

func TestReverserReverseMissingParamFails(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/{id}", noopView, Name("detail"))}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	rv, err := routes.Reverser()
	if err != nil {
		t.Fatalf("Reverser() returned error: %v", err)
	}

	url, err := rv.Reverse("users:detail", Params{})
	if err == nil {
		t.Fatalf("Reverse returned nil error for missing param, want non-nil (got url %q)", url)
	}
}

func TestRouteRegistryReverserBeforeRunRegistrationFails(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/{id}", noopView, Name("detail"))}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}

	if _, err := routes.Reverser(); err == nil {
		t.Fatal("Reverser() returned nil error before RunRegistration, want non-nil")
	}
}

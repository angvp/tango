package tango

import "testing"

func noopView(*Context) error { return nil }

func TestRouteRegistryIncludeNamespacesRouteNames(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()

	err := routes.Include("/users/", []Route{
		Path("GET", "/{id}", noopView, Name("detail")),
	})
	if err != nil {
		t.Fatalf("Include returned error: %v", err)
	}

	names := routes.IncludedNames()
	if len(names) != 1 || names[0] != "users:detail" {
		t.Fatalf("IncludedNames() = %v, want [%q]", names, "users:detail")
	}
}

func TestRouteRegistryIncludeNormalizesTrailingSlash(t *testing.T) {
	withSlash := NewRegistry().Routes()
	if err := withSlash.Include("/users/", []Route{Path("GET", "/{id}", noopView)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}

	withoutSlash := NewRegistry().Routes()
	if err := withoutSlash.Include("/users", []Route{Path("GET", "/{id}", noopView)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}

	got := withSlash.IncludedPatterns()
	want := withoutSlash.IncludedPatterns()
	if len(got) != 1 || len(want) != 1 || got[0] != want[0] {
		t.Fatalf("mounted patterns differ: %v vs %v", got, want)
	}
}

func TestRouteRegistryIncludeMalformedPatternFails(t *testing.T) {
	routes := NewRegistry().Routes()

	err := routes.Include("/users/", []Route{
		Path("GET", "/{id", noopView),
	})
	if err == nil {
		t.Fatal("Include returned nil error for malformed pattern, want non-nil")
	}
}

func TestRouteRegistryIncludeDuplicateNameWithinCallFails(t *testing.T) {
	routes := NewRegistry().Routes()

	err := routes.Include("/users/", []Route{
		Path("GET", "/{id}", noopView, Name("detail")),
		Path("DELETE", "/{id}", noopView, Name("detail")),
	})
	if err == nil {
		t.Fatal("Include returned nil error for duplicate name within call, want non-nil")
	}
}

func TestRouteRegistryIncludeDoesNotDetectCrossCallDuplicates(t *testing.T) {
	routes := NewRegistry().Routes()

	if err := routes.Include("/users/", []Route{
		Path("GET", "/{id}", noopView, Name("detail")),
	}); err != nil {
		t.Fatalf("first Include returned error: %v", err)
	}

	if err := routes.Include("/users/", []Route{
		Path("GET", "/{id}/profile", noopView, Name("detail")),
	}); err != nil {
		t.Fatalf("second Include returned error: %v, want nil (cross-call detection is ticket 07's job)", err)
	}
}

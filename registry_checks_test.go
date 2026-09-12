package tango

import "testing"

func TestRegistryChecksAggregatesOnlyFromCheckerApps(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(checksApp{
		name:   "widgets",
		checks: []AppCheck{{Description: "widgets ok"}},
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := registry.Register(NewApp("plain", func(*Registry) error { return nil })); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := registry.Register(checksApp{
		name:   "gadgets",
		checks: []AppCheck{{Description: "gadgets ok"}, {Description: "gadgets fast"}},
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	got := registry.Checks()
	if len(got) != 3 {
		t.Fatalf("Checks() returned %d checks, want 3", len(got))
	}

	want := []string{"widgets ok", "gadgets ok", "gadgets fast"}
	for i, description := range want {
		if got[i].Description != description {
			t.Fatalf("Checks()[%d].Description = %q, want %q (order not preserved)", i, got[i].Description, description)
		}
	}
}

func TestRegistryChecksEmptyWhenNoAppImplementsChecker(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(NewApp("plain", func(*Registry) error { return nil })); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	got := registry.Checks()
	if len(got) != 0 {
		t.Fatalf("Checks() returned %d checks, want 0", len(got))
	}
}

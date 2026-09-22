package tango

import (
	"context"
	"errors"
	"testing"
)

func TestRegisterLifecycleRejectsBlankName(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			err := r.RegisterLifecycle(Lifecycle{Name: tt.in, Stop: func(context.Context) error { return nil }})
			if err == nil {
				t.Fatal("expected an error for a blank name, got nil")
			}
			if len(r.Lifecycles()) != 0 {
				t.Fatal("a rejected registration must not be recorded")
			}
		})
	}
}

func TestRegisterLifecycleRejectsBothCallbacksNil(t *testing.T) {
	r := NewRegistry()
	err := r.RegisterLifecycle(Lifecycle{Name: "no-op"})
	if err == nil {
		t.Fatal("expected an error when both Start and Stop are nil, got nil")
	}
	if len(r.Lifecycles()) != 0 {
		t.Fatal("a rejected registration must not be recorded")
	}
}

func TestRegisterLifecycleAllowsStartOnlyOrStopOnly(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterLifecycle(Lifecycle{Name: "start-only", Start: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("Start-only lifecycle must be accepted: %v", err)
	}
	if err := r.RegisterLifecycle(Lifecycle{Name: "stop-only", Stop: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("Stop-only lifecycle must be accepted: %v", err)
	}
	if len(r.Lifecycles()) != 2 {
		t.Fatalf("Lifecycles() = %d entries, want 2", len(r.Lifecycles()))
	}
}

func TestRegisterLifecycleRejectsDuplicateNameWithoutCallingStart(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterLifecycle(Lifecycle{Name: "dup", Start: func(context.Context) error {
		t.Fatal("Start must never be called by RegisterLifecycle")
		return nil
	}}); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	err := r.RegisterLifecycle(Lifecycle{Name: "dup", Stop: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("expected an error for a duplicate name, got nil")
	}
	if !errors.Is(err, ErrDuplicateLifecycle) {
		t.Fatalf("err = %v, want errors.Is match against ErrDuplicateLifecycle", err)
	}
	if len(r.Lifecycles()) != 1 {
		t.Fatalf("Lifecycles() = %d entries, want 1 (the duplicate must not be recorded)", len(r.Lifecycles()))
	}
}

func TestLifecyclesPreservesRegistrationOrder(t *testing.T) {
	r := NewRegistry()
	names := []string{"a", "b", "c"}
	for _, name := range names {
		if err := r.RegisterLifecycle(Lifecycle{Name: name, Stop: func(context.Context) error { return nil }}); err != nil {
			t.Fatalf("RegisterLifecycle(%q): %v", name, err)
		}
	}

	got := r.Lifecycles()
	if len(got) != len(names) {
		t.Fatalf("Lifecycles() = %d entries, want %d", len(got), len(names))
	}
	for i, name := range names {
		if got[i].Name != name {
			t.Fatalf("Lifecycles()[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}

// lifecycleRegisteringApp registers a Lifecycle from its own Register
// callback, mirroring how a real App would wire up a background component
// (e.g. realtime.Hub) during InstalledApps registration.
type lifecycleRegisteringApp struct {
	name          string
	lifecycleName string
}

func (a lifecycleRegisteringApp) Name() string { return a.name }

func (a lifecycleRegisteringApp) Register(r *Registry) error {
	return r.RegisterLifecycle(Lifecycle{Name: a.lifecycleName, Stop: func(context.Context) error { return nil }})
}

func TestLifecyclesPreservesOrderAcrossAppRegisterCallbacks(t *testing.T) {
	r := NewRegistry()
	apps := []lifecycleRegisteringApp{
		{name: "app-a", lifecycleName: "lifecycle-a"},
		{name: "app-b", lifecycleName: "lifecycle-b"},
		{name: "app-c", lifecycleName: "lifecycle-c"},
	}
	for _, app := range apps {
		if err := r.Register(app); err != nil {
			t.Fatalf("Register(%q): %v", app.Name(), err)
		}
	}

	if err := r.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration: %v", err)
	}

	got := r.Lifecycles()
	want := []string{"lifecycle-a", "lifecycle-b", "lifecycle-c"}
	if len(got) != len(want) {
		t.Fatalf("Lifecycles() = %d entries, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("Lifecycles()[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}

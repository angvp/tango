package tango

import (
	"errors"
	"testing"
)

var errBoom = errors.New("boom")

func TestRegistryRegisterSucceeds(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(NewApp("users", func(*Registry) error {
		return nil
	}))

	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
}

func TestRegistryRegisterDuplicateNameFails(t *testing.T) {
	registry := NewRegistry()
	noop := func(*Registry) error { return nil }

	if err := registry.Register(NewApp("users", noop)); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	err := registry.Register(NewApp("users", noop))

	if !errors.Is(err, ErrDuplicateApp) {
		t.Fatalf("Register error = %v, want errors.Is(err, ErrDuplicateApp)", err)
	}
}

func TestRegistryRegisterDuplicateDoesNotInvokeRegisterFn(t *testing.T) {
	registry := NewRegistry()
	firstCalled := false
	secondCalled := false

	if err := registry.Register(NewApp("users", func(*Registry) error {
		firstCalled = true
		return nil
	})); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	_ = registry.Register(NewApp("users", func(*Registry) error {
		secondCalled = true
		return nil
	}))

	if firstCalled || secondCalled {
		t.Fatal("Registry.Register invoked a registerFn as a side effect")
	}
}

func TestRegistryRunRegistrationInvokesAllApps(t *testing.T) {
	registry := NewRegistry()
	calls := map[string]int{}

	for _, name := range []string{"accounts", "users", "billing"} {
		name := name
		if err := registry.Register(NewApp(name, func(*Registry) error {
			calls[name]++
			return nil
		})); err != nil {
			t.Fatalf("Register(%q) returned error: %v", name, err)
		}
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	for name, count := range calls {
		if count != 1 {
			t.Fatalf("app %q was invoked %d times, want 1", name, count)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("got %d apps invoked, want 3", len(calls))
	}
}

func TestRegistryRunRegistrationRespectsOrder(t *testing.T) {
	registry := NewRegistry()
	var order []string

	for _, name := range []string{"accounts", "users", "billing"} {
		name := name
		if err := registry.Register(NewApp(name, func(*Registry) error {
			order = append(order, name)
			return nil
		})); err != nil {
			t.Fatalf("Register(%q) returned error: %v", name, err)
		}
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	want := []string{"accounts", "users", "billing"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i, name := range want {
		if order[i] != name {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestRegistryRunRegistrationLaterAppSeesEarlierAppState verifies that every
// app's callback receives the same *Registry instance. Since no registration
// sub-API (models, routes, etc.) exists yet at this milestone, instance
// identity is the observable mechanism by which a later app could see state
// an earlier app registered.
func TestRegistryRunRegistrationLaterAppSeesEarlierAppState(t *testing.T) {
	registry := NewRegistry()
	var seenByFirst *Registry

	if err := registry.Register(NewApp("accounts", func(r *Registry) error {
		seenByFirst = r
		return nil
	})); err != nil {
		t.Fatalf("Register(accounts) returned error: %v", err)
	}

	if err := registry.Register(NewApp("users", func(r *Registry) error {
		if seenByFirst == nil {
			t.Fatal("accounts callback has not run yet when users callback runs")
		}
		if r != seenByFirst {
			t.Fatal("users callback did not receive the same Registry instance as accounts")
		}
		return nil
	})); err != nil {
		t.Fatalf("Register(users) returned error: %v", err)
	}

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
}

func TestRegistryRunRegistrationStopsOnFirstError(t *testing.T) {
	registry := NewRegistry()
	thirdCalled := false

	if err := registry.Register(NewApp("accounts", func(*Registry) error {
		return nil
	})); err != nil {
		t.Fatalf("Register(accounts) returned error: %v", err)
	}

	if err := registry.Register(NewApp("users", func(*Registry) error {
		return errBoom
	})); err != nil {
		t.Fatalf("Register(users) returned error: %v", err)
	}

	if err := registry.Register(NewApp("billing", func(*Registry) error {
		thirdCalled = true
		return nil
	})); err != nil {
		t.Fatalf("Register(billing) returned error: %v", err)
	}

	err := registry.RunRegistration()

	if !errors.Is(err, errBoom) {
		t.Fatalf("RunRegistration error = %v, want errBoom", err)
	}
	if thirdCalled {
		t.Fatal("RunRegistration invoked an app registered after the failing one")
	}
}

func TestRegistryRunRegistrationEmptyRegistrySucceeds(t *testing.T) {
	registry := NewRegistry()

	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
}

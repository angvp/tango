package tango

import "testing"

func TestNewAppReturnsNamedApp(t *testing.T) {
	app := NewApp("users", func(*Registry) error {
		return nil
	})

	if app.Name() != "users" {
		t.Fatalf("Name() = %q, want %q", app.Name(), "users")
	}
}

func TestNewAppDoesNotInvokeRegisterFnAtConstruction(t *testing.T) {
	called := false

	NewApp("users", func(*Registry) error {
		called = true
		return nil
	})

	if called {
		t.Fatal("NewApp invoked registerFn during construction")
	}
}

func TestAppRegisterInvokesRegisterFn(t *testing.T) {
	registry := &Registry{}
	called := false

	app := NewApp("users", func(got *Registry) error {
		called = true
		if got != registry {
			t.Fatal("Register passed a different registry")
		}
		return nil
	})

	if err := app.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if !called {
		t.Fatal("Register did not invoke registerFn")
	}
}

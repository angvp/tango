package tango_test

import (
	"errors"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/storagetest"
)

func TestRegistryHoldsOneObjectStore(t *testing.T) {
	registry := tango.NewRegistry()
	if store, ok := registry.Storage(); ok || store != nil {
		t.Fatalf("Storage() = %v, %v on a new registry, want nil, false", store, ok)
	}

	first := storagetest.NewMemory()
	if err := registry.SetStorage(first); err != nil {
		t.Fatalf("SetStorage: %v", err)
	}

	if store, ok := registry.Storage(); !ok || store != first {
		t.Fatalf("Storage() = %v, %v, want the store that was set", store, ok)
	}
}

func TestRegistrySetStorageTwiceIsRefusedAndKeepsTheFirst(t *testing.T) {
	registry := tango.NewRegistry()
	first := storagetest.NewMemory()
	if err := registry.SetStorage(first); err != nil {
		t.Fatal(err)
	}

	err := registry.SetStorage(storagetest.NewMemory())

	if !errors.Is(err, tango.ErrStorageAlreadySet) {
		t.Fatalf("second SetStorage = %v, want ErrStorageAlreadySet", err)
	}
	if store, _ := registry.Storage(); store != first {
		t.Fatal("a refused SetStorage replaced the store")
	}
}

func TestRegistrySetStorageRefusesNil(t *testing.T) {
	if err := tango.NewRegistry().SetStorage(nil); err == nil {
		t.Fatal("SetStorage(nil) = nil, want an error")
	}
}

func TestAnAppSetsTheStoreFromItsRegistrationAndAnotherCannotReplaceIt(t *testing.T) {
	first := storagetest.NewMemory()
	registry := tango.NewRegistry()
	for _, app := range []tango.App{
		tango.NewApp("media", func(r *tango.Registry) error { return r.SetStorage(first) }),
		tango.NewApp("other", func(r *tango.Registry) error { return r.SetStorage(storagetest.NewMemory()) }),
	} {
		if err := registry.Register(app); err != nil {
			t.Fatal(err)
		}
	}

	err := registry.RunRegistration()

	if !errors.Is(err, tango.ErrStorageAlreadySet) {
		t.Fatalf("RunRegistration = %v, want ErrStorageAlreadySet from the second app", err)
	}
	if store, _ := registry.Storage(); store != first {
		t.Fatal("the second app's store replaced the first")
	}
}

package admin

import (
	"testing"

	"github.com/angvp/tango/model"
)

type widget struct {
	ID     int64 `tango:"pk"`
	Name   string
	Active bool
}

func TestAdminRegisterStoresValidOptions(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{
		ListDisplay: []string{"Name", "Active"},
		Search:      []string{"Name"},
		Ordering:    []string{"Name"},
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	registration, ok := registry.Get("widget")
	if !ok {
		t.Fatal("Get returned false after Register")
	}
	if len(registration.Options.ListDisplay) != 2 {
		t.Fatalf("ListDisplay = %v, want 2 entries", registration.Options.ListDisplay)
	}
}

func TestAdminRegisterUnknownListDisplayFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{ListDisplay: []string{"NoSuchField"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown ListDisplay field, want non-nil")
	}
	if _, ok := registry.Get("widget"); ok {
		t.Fatal("Get returned true after a failed Register")
	}
}

func TestAdminRegisterUnknownSearchFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{Search: []string{"NoSuchField"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown Search field, want non-nil")
	}
	if _, ok := registry.Get("widget"); ok {
		t.Fatal("Get returned true after a failed Register")
	}
}

func TestAdminRegisterUnknownOrderingFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{Ordering: []string{"NoSuchField"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown Ordering field, want non-nil")
	}
	if _, ok := registry.Get("widget"); ok {
		t.Fatal("Get returned true after a failed Register")
	}
}

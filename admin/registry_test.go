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

func TestAdminRegisterStoresValidLabel(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	if err := registry.Register(widget{}, Options{Label: "Name"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	registration, ok := registry.Get("widget")
	if !ok {
		t.Fatal("Get returned false after Register")
	}
	if registration.Options.Label != "Name" {
		t.Fatalf("Label = %q, want %q", registration.Options.Label, "Name")
	}
}

func TestAdminRegisterUnknownLabelFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{Label: "NoSuchField"})
	if err == nil {
		t.Fatal("Register returned nil error for unknown Label field, want non-nil")
	}
	if _, ok := registry.Get("widget"); ok {
		t.Fatal("Get returned true after a failed Register")
	}
}

func TestAdminRegisterEmptyLabelIsValid(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	if err := registry.Register(widget{}, Options{}); err != nil {
		t.Fatalf("Register returned error for empty Label: %v", err)
	}
}

func TestAdminRegisterStoresValidLabelsHelpTextReadOnlyFieldOrder(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{
		Labels:     map[string]string{"Name": "Widget name"},
		HelpText:   map[string]string{"Active": "Whether this widget is in use."},
		ReadOnly:   []string{"Name"},
		FieldOrder: []string{"Active", "Name"},
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	registration, _ := registry.Get("widget")
	if registration.Options.Labels["Name"] != "Widget name" {
		t.Fatalf("Labels[Name] = %q, want %q", registration.Options.Labels["Name"], "Widget name")
	}
}

func TestAdminRegisterUnknownLabelsFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{Labels: map[string]string{"NoSuchField": "x"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown Labels field, want non-nil")
	}
}

func TestAdminRegisterUnknownHelpTextFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{HelpText: map[string]string{"NoSuchField": "x"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown HelpText field, want non-nil")
	}
}

func TestAdminRegisterUnknownReadOnlyFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{ReadOnly: []string{"NoSuchField"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown ReadOnly field, want non-nil")
	}
}

func TestAdminRegisterUnknownFieldOrderFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{FieldOrder: []string{"NoSuchField"}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown FieldOrder field, want non-nil")
	}
}

func TestAdminRegisterUnknownWidgetsFieldFails(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{Widgets: map[string]Widget{"NoSuchField": Textarea()}})
	if err == nil {
		t.Fatal("Register returned nil error for unknown Widgets field, want non-nil")
	}
}

func TestAdminRegisterStoresValidWidgets(t *testing.T) {
	registry := NewRegistry(model.NewRegistry())

	err := registry.Register(widget{}, Options{Widgets: map[string]Widget{"Name": Textarea()}})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	registration, _ := registry.Get("widget")
	if registration.Options.Widgets["Name"] == nil {
		t.Fatal("Widgets[Name] is nil, want the registered Textarea widget")
	}
}

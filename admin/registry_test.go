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

// TestAdminRegisterValidatesOptionsFields exercises Register's field
// validation across every Options field: each field naming an unknown
// struct field must fail Register (and, where the original test checked
// it, leave nothing behind in the registry), while valid values must be
// stored verbatim and retrievable via Get.
func TestAdminRegisterValidatesOptionsFields(t *testing.T) {
	tests := []struct {
		name string
		// options is what gets passed to Register.
		options Options
		// wantErr is whether Register is expected to return a non-nil error.
		wantErr bool
		// checkAbsentOnFailure additionally asserts that Get reports the
		// model as not registered after a failed Register. Only some of the
		// original failure tests checked this.
		checkAbsentOnFailure bool
		// verify runs on success cases that checked more than "err == nil";
		// it receives the stored registration.
		verify func(t *testing.T, registration ModelRegistration)
	}{
		{
			name:    "valid ListDisplay and Search and Ordering fields are stored",
			options: Options{ListDisplay: []string{"Name", "Active"}, Search: []string{"Name"}, Ordering: []string{"Name"}},
			verify: func(t *testing.T, registration ModelRegistration) {
				if len(registration.Options.ListDisplay) != 2 {
					t.Fatalf("ListDisplay = %v, want 2 entries", registration.Options.ListDisplay)
				}
			},
		},
		{
			name:                 "unknown ListDisplay field fails registration",
			options:              Options{ListDisplay: []string{"NoSuchField"}},
			wantErr:              true,
			checkAbsentOnFailure: true,
		},
		{
			name:                 "unknown Search field fails registration",
			options:              Options{Search: []string{"NoSuchField"}},
			wantErr:              true,
			checkAbsentOnFailure: true,
		},
		{
			name:                 "unknown Ordering field fails registration",
			options:              Options{Ordering: []string{"NoSuchField"}},
			wantErr:              true,
			checkAbsentOnFailure: true,
		},
		{
			name:    "descending ordering syntax (-Name) is accepted and preserved verbatim",
			options: Options{Ordering: []string{"-Name"}},
			verify: func(t *testing.T, registration ModelRegistration) {
				if len(registration.Options.Ordering) != 1 || registration.Options.Ordering[0] != "-Name" {
					t.Fatalf("Ordering = %v, want [\"-Name\"] preserved verbatim", registration.Options.Ordering)
				}
			},
		},
		{
			name:                 "unknown descending Ordering field fails registration",
			options:              Options{Ordering: []string{"-NoSuchField"}},
			wantErr:              true,
			checkAbsentOnFailure: true,
		},
		{
			name:    "valid Label is stored",
			options: Options{Label: "Name"},
			verify: func(t *testing.T, registration ModelRegistration) {
				if registration.Options.Label != "Name" {
					t.Fatalf("Label = %q, want %q", registration.Options.Label, "Name")
				}
			},
		},
		{
			name:                 "unknown Label field fails registration",
			options:              Options{Label: "NoSuchField"},
			wantErr:              true,
			checkAbsentOnFailure: true,
		},
		{
			name:    "empty Label is valid",
			options: Options{},
		},
		{
			name: "valid Labels, HelpText, ReadOnly, and FieldOrder are stored",
			options: Options{
				Labels:     map[string]string{"Name": "Widget name"},
				HelpText:   map[string]string{"Active": "Whether this widget is in use."},
				ReadOnly:   []string{"Name"},
				FieldOrder: []string{"Active", "Name"},
			},
			verify: func(t *testing.T, registration ModelRegistration) {
				if registration.Options.Labels["Name"] != "Widget name" {
					t.Fatalf("Labels[Name] = %q, want %q", registration.Options.Labels["Name"], "Widget name")
				}
			},
		},
		{
			name:    "unknown Labels field fails registration",
			options: Options{Labels: map[string]string{"NoSuchField": "x"}},
			wantErr: true,
		},
		{
			name:    "unknown HelpText field fails registration",
			options: Options{HelpText: map[string]string{"NoSuchField": "x"}},
			wantErr: true,
		},
		{
			name:    "unknown ReadOnly field fails registration",
			options: Options{ReadOnly: []string{"NoSuchField"}},
			wantErr: true,
		},
		{
			name:    "unknown FieldOrder field fails registration",
			options: Options{FieldOrder: []string{"NoSuchField"}},
			wantErr: true,
		},
		{
			name:    "unknown Widgets field fails registration",
			options: Options{Widgets: map[string]Widget{"NoSuchField": Textarea()}},
			wantErr: true,
		},
		{
			name:    "valid Widgets entry is stored",
			options: Options{Widgets: map[string]Widget{"Name": Textarea()}},
			verify: func(t *testing.T, registration ModelRegistration) {
				if registration.Options.Widgets["Name"] == nil {
					t.Fatal("Widgets[Name] is nil, want the registered Textarea widget")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(model.NewRegistry())

			err := registry.Register(widget{}, tt.options)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("%s: Register returned nil error, want non-nil", tt.name)
				}
				if tt.checkAbsentOnFailure {
					if _, ok := registry.Get("widget"); ok {
						t.Fatalf("%s: Get returned true after a failed Register", tt.name)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("%s: Register returned error: %v", tt.name, err)
			}
			if tt.verify != nil {
				registration, ok := registry.Get("widget")
				if !ok {
					t.Fatalf("%s: Get returned false after Register", tt.name)
				}
				tt.verify(t, registration)
			}
		})
	}
}

package adminregistry

import (
	"strings"
	"testing"

	"github.com/angvp/tango/model"
)

type RegistryTestPost struct {
	ID     int64 `tango:"pk"`
	Title  string
	Author string
	Status string
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(model.NewRegistry())
}

func TestRegisterAcceptsValidOptions(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{
		ListDisplay: []string{"Title", "Status"},
		Search:      []string{"Title"},
		Ordering:    []string{"-Title"},
		Label:       "Title",
		Labels:      map[string]string{"Title": "Post title"},
		HelpText:    map[string]string{"Title": "The post's title"},
		ReadOnly:    []string{"Status"},
		FieldOrder:  []string{"Title", "Status"},
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
}

func TestRegisterRejectsNonStringSearchField(t *testing.T) {
	r := newTestRegistry(t)
	err := r.Register(RegistryTestPost{}, Options{Search: []string{"ID"}})
	if err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("Register error = %v, want string-field validation", err)
	}
}

func TestRegisterStoresRegistrationUnderModelName(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.Register(RegistryTestPost{}, Options{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, ok := r.Get("RegistryTestPost")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.Model.Name != "RegistryTestPost" {
		t.Fatalf("got.Model.Name = %q, want %q", got.Model.Name, "RegistryTestPost")
	}
}

func TestRegisterAutoRegistersModelWhenNotAlreadyKnown(t *testing.T) {
	models := model.NewRegistry()
	r := NewRegistry(models)

	if err := r.Register(RegistryTestPost{}, Options{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if _, ok := models.Get("RegistryTestPost"); !ok {
		t.Fatal("underlying model.Registry does not have RegistryTestPost registered")
	}
}

func TestRegisterReusesAlreadyRegisteredModel(t *testing.T) {
	models := model.NewRegistry()
	if err := models.Register(RegistryTestPost{}); err != nil {
		t.Fatalf("models.Register() error = %v", err)
	}
	r := NewRegistry(models)

	if err := r.Register(RegistryTestPost{}, Options{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, ok := r.Get("RegistryTestPost")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if len(got.Model.Fields) == 0 {
		t.Fatal("got.Model.Fields is empty, want fields from the reused model metadata")
	}
}

func TestRegistrationsReturnsEveryRegisteredModel(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Register(RegistryTestPost{}, Options{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	regs := r.Registrations()
	if len(regs) != 1 {
		t.Fatalf("len(Registrations()) = %d, want 1", len(regs))
	}
	if regs[0].Model.Name != "RegistryTestPost" {
		t.Fatalf("Registrations()[0].Model.Name = %q, want %q", regs[0].Model.Name, "RegistryTestPost")
	}
}

func TestRegisterRejectsUnknownFieldInEachOption(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "ListDisplay",
			opts: Options{ListDisplay: []string{"Bogus"}},
			want: "ListDisplay references unknown field",
		},
		{
			name: "Search",
			opts: Options{Search: []string{"Bogus"}},
			want: "Search references unknown field",
		},
		{
			name: "Ordering",
			opts: Options{Ordering: []string{"Bogus"}},
			want: "Ordering references unknown field",
		},
		{
			name: "Ordering with descending prefix",
			opts: Options{Ordering: []string{"-Bogus"}},
			want: "Ordering references unknown field",
		},
		{
			name: "Label",
			opts: Options{Label: "Bogus"},
			want: "Label references unknown field",
		},
		{
			name: "Labels key",
			opts: Options{Labels: map[string]string{"Bogus": "x"}},
			want: "Labels references unknown field",
		},
		{
			name: "HelpText key",
			opts: Options{HelpText: map[string]string{"Bogus": "x"}},
			want: "HelpText references unknown field",
		},
		{
			name: "ReadOnly",
			opts: Options{ReadOnly: []string{"Bogus"}},
			want: "ReadOnly references unknown field",
		},
		{
			name: "FieldOrder",
			opts: Options{FieldOrder: []string{"Bogus"}},
			want: "FieldOrder references unknown field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRegistry(t)
			err := r.Register(RegistryTestPost{}, tc.opts)
			if err == nil {
				t.Fatalf("Register() error = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Register() error = %q, want it to contain %q", err.Error(), tc.want)
			}
			if _, ok := r.Get("RegistryTestPost"); ok {
				t.Fatal("Get() ok = true, want false — a failed Register must not store a registration")
			}
		})
	}
}

func TestRegisterOrderingStripsDescendingPrefixBeforeValidating(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.Register(RegistryTestPost{}, Options{Ordering: []string{"-Title", "Status"}}); err != nil {
		t.Fatalf("Register() error = %v, want nil (valid fields with descending prefix)", err)
	}
}

func TestRegisterEmptyOptionsAreAlwaysValid(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.Register(RegistryTestPost{}, Options{}); err != nil {
		t.Fatalf("Register() error = %v, want nil for zero-value Options", err)
	}
}

func TestGetReturnsFalseForUnknownModel(t *testing.T) {
	r := newTestRegistry(t)

	if _, ok := r.Get("Nope"); ok {
		t.Fatal("Get(\"Nope\") ok = true, want false")
	}
}

func TestRegisterWithPointerValueDerivesSameModelName(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.Register(&RegistryTestPost{}, Options{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, ok := r.Get("RegistryTestPost"); !ok {
		t.Fatal("Get() ok = false, want true — pointer and value should derive the same model name")
	}
}

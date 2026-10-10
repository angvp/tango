package adminregistry

import (
	"context"
	"strings"
	"testing"
)

func displayFor(context.Context, any) Display { return Display{Text: "x"} }

func TestRegisterAcceptsSyntheticListColumnWithDisplay(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{
		ListDisplay: []string{"Title", "Summary"},
		Display:     map[string]DisplayFunc{"Summary": displayFor},
		Labels:      map[string]string{"Summary": "Summary"},
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
}

func TestRegisterRejectsSyntheticListColumnWithoutDisplay(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{ListDisplay: []string{"Summary"}})
	if err == nil || !strings.Contains(err.Error(), `ListDisplay references unknown field "Summary"`) {
		t.Fatalf("Register error = %v, want unknown ListDisplay field", err)
	}
}

func TestRegisterRejectsDisplayKeyAbsentFromListDisplay(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{
		ListDisplay: []string{"Title"},
		Display:     map[string]DisplayFunc{"Status": displayFor},
	})
	if err == nil || !strings.Contains(err.Error(), `Display references "Status"`) {
		t.Fatalf("Register error = %v, want Display-not-in-ListDisplay error", err)
	}
}

func TestRegisterRejectsNilDisplayFunc(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{
		ListDisplay: []string{"Title"},
		Display:     map[string]DisplayFunc{"Title": nil},
	})
	if err == nil || !strings.Contains(err.Error(), `Display "Title" on RegistryTestPost is nil`) {
		t.Fatalf("Register error = %v, want nil-func error", err)
	}
}

func TestRegisterRejectsUnknownLabelsKeyThatIsNotASyntheticColumn(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{
		ListDisplay: []string{"Title"},
		Labels:      map[string]string{"Summary": "Summary"},
	})
	if err == nil || !strings.Contains(err.Error(), `Labels references unknown field "Summary"`) {
		t.Fatalf("Register error = %v, want unknown Labels field", err)
	}
}

func TestRegisterLabelsSyntheticKeyNeedsListDisplayMembership(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Register(RegistryTestPost{}, Options{
		ListDisplay: []string{"Title"},
		Display:     map[string]DisplayFunc{"Summary": displayFor},
		Labels:      map[string]string{"Summary": "Summary"},
	})
	if err == nil || !strings.Contains(err.Error(), `Display references "Summary"`) {
		t.Fatalf("Register error = %v, want Display-not-in-ListDisplay error", err)
	}
}

func TestRegisterSyntheticColumnDoesNotLoosenOtherOptions(t *testing.T) {
	cases := map[string]Options{
		"Search":     {ListDisplay: []string{"Summary"}, Display: map[string]DisplayFunc{"Summary": displayFor}, Search: []string{"Summary"}},
		"Ordering":   {ListDisplay: []string{"Summary"}, Display: map[string]DisplayFunc{"Summary": displayFor}, Ordering: []string{"Summary"}},
		"HelpText":   {ListDisplay: []string{"Summary"}, Display: map[string]DisplayFunc{"Summary": displayFor}, HelpText: map[string]string{"Summary": "x"}},
		"ReadOnly":   {ListDisplay: []string{"Summary"}, Display: map[string]DisplayFunc{"Summary": displayFor}, ReadOnly: []string{"Summary"}},
		"FieldOrder": {ListDisplay: []string{"Summary"}, Display: map[string]DisplayFunc{"Summary": displayFor}, FieldOrder: []string{"Summary"}},
		"Label":      {ListDisplay: []string{"Summary"}, Display: map[string]DisplayFunc{"Summary": displayFor}, Label: "Summary"},
	}
	for option, opts := range cases {
		t.Run(option, func(t *testing.T) {
			if err := newTestRegistry(t).Register(RegistryTestPost{}, opts); err == nil {
				t.Fatalf("Register accepted synthetic name in %s, want an unknown-field error", option)
			}
		})
	}
}

package adminregistry

import (
	"errors"
	"testing"

	"github.com/angvp/tango/model"
)

func TestDescribedByListsHelpThenErrorIds(t *testing.T) {
	tests := []struct {
		name string
		f    FieldContext
		want string
	}{
		{name: "neither", f: FieldContext{Name: "title"}, want: ""},
		{name: "help text only", f: FieldContext{Name: "title", HelpText: "Short"}, want: "field-title-help"},
		{name: "error only", f: FieldContext{Name: "title", Error: "Required"}, want: "field-title-error"},
		{name: "both", f: FieldContext{Name: "title", HelpText: "Short", Error: "Required"}, want: "field-title-help field-title-error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.DescribedBy(); got != tt.want {
				t.Fatalf("DescribedBy() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestFindFieldReportsAMissingName(t *testing.T) {
	meta := model.ModelMeta{Fields: []model.FieldMeta{{Name: "Title"}}}
	if field, ok := findField(meta, "Title"); !ok || field.Name != "Title" {
		t.Fatalf("findField(Title) = %q, %v; want Title, true", field.Name, ok)
	}
	if field, ok := findField(meta, "Body"); ok || field.Name != "" {
		t.Fatalf("findField(Body) = %q, %v; want zero value, false", field.Name, ok)
	}
}

// RegistryEmbeddedBase is exported: the model registry skips unexported
// embedded fields, so only an exported embedding reaches the rejection.
type RegistryEmbeddedBase struct {
	Stamp int64
}

type registryEmbeddedPost struct {
	RegistryEmbeddedBase
	ID    int64 `tango:"pk"`
	Title string
}

type registryChanPost struct {
	ID int64 `tango:"pk"`
	C  chan int
}

type registryBadTagPost struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=abc"`
}

func TestRegisterReportsAModelThatTheModelRegistryRejects(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  error
	}{
		{name: "embedded field", value: registryEmbeddedPost{}, want: model.ErrEmbeddedField},
		{name: "unsupported field type", value: registryChanPost{}, want: model.ErrUnsupportedField},
		{name: "invalid field tag", value: registryBadTagPost{}, want: model.ErrInvalidFieldTag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRegistry(t)
			err := r.Register(tt.value, Options{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Register() error = %v; want %v", err, tt.want)
			}
			if _, ok := r.Get(modelName(tt.value)); ok {
				t.Fatal("a rejected model was stored")
			}
		})
	}
}

func TestRegisterRejectsAWidgetKeyThatIsNotAField(t *testing.T) {
	r := newTestRegistry(t)
	err := r.Register(RegistryTestPost{}, Options{Widgets: map[string]Widget{"Missing": nil}})
	if err == nil || err.Error() != `tango admin: Widgets references unknown field "Missing" on RegistryTestPost` {
		t.Fatalf("Register() error = %v; want the unknown Widgets field error", err)
	}
}

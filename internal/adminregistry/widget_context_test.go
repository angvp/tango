package adminregistry

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/model"
)

type ctxKey struct{}

func TestContextOfReturnsTheContextAttachedByWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "staff")
	got := ContextOf(WithContext(FieldContext{Name: "title"}, ctx))
	if got != ctx {
		t.Fatalf("ContextOf = %v; want the attached context", got)
	}
	if v := got.Value(ctxKey{}); v != "staff" {
		t.Fatalf("attached value = %v; want staff", v)
	}
}

func TestContextOfFallsBackToBackgroundWhenNoneWasAttached(t *testing.T) {
	tests := []struct {
		name string
		f    FieldContext
	}{
		{name: "zero FieldContext", f: FieldContext{}},
		{name: "nil context attached", f: WithContext(FieldContext{Name: "title"}, nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContextOf(tt.f); got != context.Background() {
				t.Fatalf("ContextOf = %v; want context.Background()", got)
			}
		})
	}
}

func TestWithContextLeavesTheOriginalFieldContextUnchanged(t *testing.T) {
	original := FieldContext{Name: "title"}
	_ = WithContext(original, context.WithValue(context.Background(), ctxKey{}, "x"))
	if got := ContextOf(original); got != context.Background() {
		t.Fatalf("original ContextOf = %v; want context.Background()", got)
	}
}

func TestWithContextLastAttachmentWins(t *testing.T) {
	first := context.WithValue(context.Background(), ctxKey{}, "first")
	second := context.WithValue(context.Background(), ctxKey{}, "second")
	got := ContextOf(WithContext(WithContext(FieldContext{}, first), second))
	if got != second {
		t.Fatalf("ContextOf = %v; want the second context", got)
	}
}

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

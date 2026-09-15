package admin

import (
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// TestInputTypeForKind covers every Go kind inputTypeForKind maps to an
// HTML input type, including the bool/checkbox and time.Time/datetime-local
// cases that never reach it through the default-widget path (bool fields
// use checkboxWidget directly, and a time.Time field there is the only
// reflect.Struct field kind tanGO's admin supports).
func TestInputTypeForKind(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"bool", reflect.TypeOf(true), "checkbox"},
		{"int", reflect.TypeOf(int(0)), "number"},
		{"float", reflect.TypeOf(float64(0)), "number"},
		{"time.Time", reflect.TypeOf(time.Time{}), "datetime-local"},
		{"string", reflect.TypeOf(""), "text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inputTypeForKind(tc.typ); got != tc.want {
				t.Errorf("inputTypeForKind(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestOrderedEditableFieldsAppendsUnlistedFieldsAfterOrderedOnes covers
// orderedEditableFields' fallback: a field absent from opts.FieldOrder is
// appended after the explicitly ordered ones, keeping its original
// relative order rather than being dropped.
func TestOrderedEditableFieldsAppendsUnlistedFieldsAfterOrderedOnes(t *testing.T) {
	meta := model.ModelMeta{
		Name: "Widget",
		Fields: []model.FieldMeta{
			{Name: "ID", PrimaryKey: true},
			{Name: "Name"},
			{Name: "Price"},
			{Name: "SKU"},
		},
	}

	got := orderedEditableFields(meta, []string{"Price"})

	want := []string{"Price", "Name", "SKU"}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("field[%d] = %q, want %q (full order: %+v)", i, got[i].Name, name, got)
		}
	}
}

// TestSetFieldFromStringUncoveredBranches covers the setFieldFromString
// branches other tests don't reach directly: a bool field (checkboxWidget
// normally handles bool itself, but the shared helper still supports it),
// blank numeric input defaulting to zero for int/uint/float fields (a user
// leaving a number field empty), and an unsupported Go kind producing an
// error rather than a panic.
func TestSetFieldFromStringUncoveredBranches(t *testing.T) {
	t.Run("bool kind sets from presence", func(t *testing.T) {
		var dest bool
		v := reflect.ValueOf(&dest).Elem()
		if err := setFieldFromString(v, v.Type(), "", true); err != nil {
			t.Fatalf("setFieldFromString: %v", err)
		}
		if !dest {
			t.Fatal("dest = false, want true when present")
		}
	})

	t.Run("blank int defaults to zero", func(t *testing.T) {
		var dest int
		v := reflect.ValueOf(&dest).Elem()
		dest = 42
		if err := setFieldFromString(v, v.Type(), "", false); err != nil {
			t.Fatalf("setFieldFromString: %v", err)
		}
		if dest != 0 {
			t.Fatalf("dest = %d, want 0 for blank input", dest)
		}
	})

	t.Run("blank uint defaults to zero", func(t *testing.T) {
		var dest uint
		v := reflect.ValueOf(&dest).Elem()
		dest = 42
		if err := setFieldFromString(v, v.Type(), "", false); err != nil {
			t.Fatalf("setFieldFromString: %v", err)
		}
		if dest != 0 {
			t.Fatalf("dest = %d, want 0 for blank input", dest)
		}
	})

	t.Run("blank float defaults to zero", func(t *testing.T) {
		var dest float64
		v := reflect.ValueOf(&dest).Elem()
		dest = 4.2
		if err := setFieldFromString(v, v.Type(), "", false); err != nil {
			t.Fatalf("setFieldFromString: %v", err)
		}
		if dest != 0 {
			t.Fatalf("dest = %v, want 0 for blank input", dest)
		}
	})

	t.Run("unsupported kind returns an error", func(t *testing.T) {
		var dest []string
		v := reflect.ValueOf(&dest).Elem()
		if err := setFieldFromString(v, v.Type(), "x", true); err == nil {
			t.Fatal("setFieldFromString on a slice field: want error, got nil")
		}
	})
}

// TestParsePKValueAndSetPKField covers every primary key Go kind
// parsePKValue/setPKField support (string, int, uint), their parse-error
// paths for a malformed URL path segment, and the unsupported-kind error
// primaryKeyField-adjacent callers rely on.
func TestParsePKValueAndSetPKField(t *testing.T) {
	t.Run("string PK", func(t *testing.T) {
		field := model.FieldMeta{Name: "Slug", Type: reflect.TypeOf("")}
		got, err := parsePKValue(field, "abc")
		if err != nil {
			t.Fatalf("parsePKValue: %v", err)
		}
		if got != "abc" {
			t.Fatalf("parsePKValue = %v, want %q", got, "abc")
		}
	})

	t.Run("int PK parse error", func(t *testing.T) {
		field := model.FieldMeta{Name: "ID", Type: reflect.TypeOf(int64(0))}
		if _, err := parsePKValue(field, "not-a-number"); err == nil {
			t.Fatal("parsePKValue: want error for non-numeric input, got nil")
		}
	})

	t.Run("uint PK", func(t *testing.T) {
		field := model.FieldMeta{Name: "ID", Type: reflect.TypeOf(uint64(0))}
		got, err := parsePKValue(field, "7")
		if err != nil {
			t.Fatalf("parsePKValue: %v", err)
		}
		if got != uint64(7) {
			t.Fatalf("parsePKValue = %v, want uint64(7)", got)
		}
	})

	t.Run("uint PK parse error", func(t *testing.T) {
		field := model.FieldMeta{Name: "ID", Type: reflect.TypeOf(uint64(0))}
		if _, err := parsePKValue(field, "not-a-number"); err == nil {
			t.Fatal("parsePKValue: want error for non-numeric input, got nil")
		}
	})

	t.Run("unsupported PK kind", func(t *testing.T) {
		field := model.FieldMeta{Name: "Flag", Type: reflect.TypeOf(true)}
		if _, err := parsePKValue(field, "true"); err == nil {
			t.Fatal("parsePKValue: want error for unsupported kind, got nil")
		}
	})

	t.Run("setPKField propagates a parse error", func(t *testing.T) {
		var dest int64
		v := reflect.ValueOf(&dest).Elem()
		field := model.FieldMeta{Name: "ID", Type: reflect.TypeOf(int64(0))}
		if err := setPKField(v, field, "nope"); err == nil {
			t.Fatal("setPKField: want error for non-numeric input, got nil")
		}
	})

	t.Run("setPKField sets a string PK", func(t *testing.T) {
		var dest string
		v := reflect.ValueOf(&dest).Elem()
		field := model.FieldMeta{Name: "Slug", Type: reflect.TypeOf("")}
		if err := setPKField(v, field, "hello"); err != nil {
			t.Fatalf("setPKField: %v", err)
		}
		if dest != "hello" {
			t.Fatalf("dest = %q, want %q", dest, "hello")
		}
	})

	t.Run("setPKField sets a uint PK", func(t *testing.T) {
		var dest uint64
		v := reflect.ValueOf(&dest).Elem()
		field := model.FieldMeta{Name: "ID", Type: reflect.TypeOf(uint64(0))}
		if err := setPKField(v, field, "9"); err != nil {
			t.Fatalf("setPKField: %v", err)
		}
		if dest != 9 {
			t.Fatalf("dest = %d, want 9", dest)
		}
	})
}

// TestPrimaryKeyFieldReturnsErrorWhenModelHasNone covers primaryKeyField's
// error path for a model metadata value with no PrimaryKey field set — a
// misconfigured or hand-built ModelMeta, since tanGO's normal model
// registration always assigns a primary key.
func TestPrimaryKeyFieldReturnsErrorWhenModelHasNone(t *testing.T) {
	meta := model.ModelMeta{Name: "NoPK", Fields: []model.FieldMeta{{Name: "Name"}}}
	if _, err := primaryKeyField(meta); err == nil {
		t.Fatal("primaryKeyField: want error for a model with no primary key, got nil")
	}
}

// TestWidgetForFieldFallsBackToNumericInputForUnresolvedForeignKey covers
// widgetForField's fallback branch: a foreign key field whose related model
// didn't resolve to a registered model (fkResolved=false) gets a plain
// numeric input instead of a select — this is the fallback path a project
// hits if ValidateForeignKeys was skipped for a field pointing at an
// unregistered model.
func TestWidgetForFieldFallsBackToNumericInputForUnresolvedForeignKey(t *testing.T) {
	field := model.FieldMeta{Name: "AuthorID", Type: reflect.TypeOf(int64(0)), ForeignKey: "Author"}
	fc := FieldContext{Name: "AuthorID"}

	widget := widgetForField(field, adminregistry.Options{}, fc, false)

	if _, ok := widget.(inputWidget); !ok {
		t.Fatalf("widgetForField = %T, want inputWidget for an unresolved foreign key", widget)
	}
}

// TestRelatedCreateURLReturnsEmptyForUnregisteredTarget covers
// relatedCreateURL's guard: a target model name the admin registry doesn't
// know about (e.g. a stale or misconfigured fk= tag) returns "" rather than
// building a broken URL.
func TestRelatedCreateURLReturnsEmptyForUnregisteredTarget(t *testing.T) {
	adminReg := adminregistry.NewRegistry(model.NewRegistry())
	if got := relatedCreateURL("NoSuchModel", adminReg, "/admin/post/new/", "AuthorID"); got != "" {
		t.Fatalf("relatedCreateURL = %q, want \"\" for an unregistered target", got)
	}
}

// TestFormatPreselectPK covers formatPreselectPK's int/uint/default
// branches: only the int case is exercised by the ordinary create-view
// tests (an int64 primary key), so this fills in uint and the
// formatFieldValue fallback (e.g. a string primary key).
func TestFormatPreselectPK(t *testing.T) {
	cases := []struct {
		name string
		v    reflect.Value
		want string
	}{
		{"uint", reflect.ValueOf(uint64(5)), "5"},
		{"string", reflect.ValueOf("slug-1"), "slug-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatPreselectPK(tc.v); got != tc.want {
				t.Errorf("formatPreselectPK(%v) = %q, want %q", tc.v, got, tc.want)
			}
		})
	}
}

// TestReadOnlyWidgetParseIsANoOp covers readOnlyWidget.Parse: a read-only
// field's Widget.Parse must never be reached in practice (populateFromForm
// special-cases ReadOnly fields before consulting the field's Widget at
// all), but the Widget interface requires an implementation, and it must
// behave as a safe no-op if ever called directly.
func TestReadOnlyWidgetParseIsANoOp(t *testing.T) {
	var dest string
	v := reflect.ValueOf(&dest).Elem()
	dest = "unchanged"

	if err := (readOnlyWidget{}).Parse(FieldContext{}, url.Values{"x": {"y"}}, v); err != nil {
		t.Fatalf("readOnlyWidget.Parse: %v", err)
	}
	if dest != "unchanged" {
		t.Fatalf("dest = %q, want unchanged", dest)
	}
}

// TestRenderWidgetTemplateReportsExecutionErrorInline covers
// renderWidgetTemplate's error branch: a template that fails to execute
// (here, one referencing a field its data doesn't have) renders an inline
// HTML comment describing the failure instead of panicking or returning a
// blank page.
func TestRenderWidgetTemplateReportsExecutionErrorInline(t *testing.T) {
	tmpl := template.Must(template.New("broken").Parse(`{{.NoSuchField}}`))

	got := renderWidgetTemplate(tmpl, struct{ Name string }{Name: "x"})

	if !strings.Contains(string(got), "tango admin: widget render error") {
		t.Fatalf("renderWidgetTemplate output = %q, want it to report the render error", got)
	}
}

// TestAppendQueryReturnsInputUnchangedForUnparsableURL covers appendQuery's
// url.Parse error branch: a raw URL string containing a character
// net/url's parser rejects (an ASCII control character) is returned
// unchanged rather than panicking or silently dropping data.
func TestAppendQueryReturnsInputUnchangedForUnparsableURL(t *testing.T) {
	broken := "/admin/post/new/\x7f"
	if got := appendQuery(broken, "k", "v"); got != broken {
		t.Fatalf("appendQuery(%q) = %q, want the input returned unchanged", broken, got)
	}
}

// TestRateLimitKeyFallsBackToRawRemoteAddr covers rateLimitKey's error
// branch: a RemoteAddr without a port (net.SplitHostPort fails) falls back
// to the raw value rather than erroring or panicking.
func TestRateLimitKeyFallsBackToRawRemoteAddr(t *testing.T) {
	r := &http.Request{RemoteAddr: "no-port-here"}
	if got := rateLimitKey(r); got != "no-port-here" {
		t.Fatalf("rateLimitKey = %q, want the raw RemoteAddr", got)
	}
}

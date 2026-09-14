package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type User struct {
	ID     int64  `tango:"pk"`
	Email  string `tango:"unique"`
	Active bool
}

func TestModelsRegisterLeavesAppEmptyWithoutSetCurrentApp(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("User")
	if meta.App != "" {
		t.Fatalf("App = %q, want empty string when SetCurrentApp was never called", meta.App)
	}
}

func TestModelsRegisterCapturesCurrentApp(t *testing.T) {
	registry := NewRegistry()
	registry.SetCurrentApp("users")
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("User")
	if meta.App != "users" {
		t.Fatalf("App = %q, want %q", meta.App, "users")
	}
}

func TestModelsRegisterTracksAppPerCallNotGlobally(t *testing.T) {
	registry := NewRegistry()
	registry.SetCurrentApp("users")
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	registry.SetCurrentApp("posts")
	if err := registry.Register(IndexedPost{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	userMeta, _ := registry.Get("User")
	postMeta, _ := registry.Get("IndexedPost")

	if userMeta.App != "users" {
		t.Fatalf("User App = %q, want %q", userMeta.App, "users")
	}
	if postMeta.App != "posts" {
		t.Fatalf("IndexedPost App = %q, want %q", postMeta.App, "posts")
	}
}

func TestRegistryAllReturnsEveryModelSortedByName(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(IndexedPost{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	all := registry.All()
	if len(all) != 2 {
		t.Fatalf("got %d models, want 2", len(all))
	}
	if all[0].Name != "IndexedPost" || all[1].Name != "User" {
		t.Fatalf("names = [%q, %q], want sorted [IndexedPost, User]", all[0].Name, all[1].Name)
	}
}

func TestRegistryAllOnEmptyRegistryReturnsEmptySlice(t *testing.T) {
	registry := NewRegistry()
	if all := registry.All(); len(all) != 0 {
		t.Fatalf("got %d models, want 0", len(all))
	}
}

func TestModelsRegisterStoresModelByName(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if _, ok := registry.Get("User"); !ok {
		t.Fatal("Get(\"User\") returned false after Register")
	}
}

func TestModelsGetUnknownNameReturnsFalse(t *testing.T) {
	registry := NewRegistry()

	meta, ok := registry.Get("Missing")
	if ok {
		t.Fatal("Get returned true for an unregistered name")
	}
	if meta.Name != "" || meta.Type != nil || meta.Fields != nil {
		t.Fatalf("Get returned non-zero ModelMeta for an unregistered name: %+v", meta)
	}
}

func TestModelsRegisterCapturesFieldNamesAndTypes(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("User")
	want := map[string]reflect.Kind{
		"ID":     reflect.Int64,
		"Email":  reflect.String,
		"Active": reflect.Bool,
	}

	if len(meta.Fields) != len(want) {
		t.Fatalf("Fields = %v, want %d entries", meta.Fields, len(want))
	}
	for _, field := range meta.Fields {
		kind, known := want[field.Name]
		if !known {
			t.Fatalf("unexpected field %q", field.Name)
		}
		if field.Type.Kind() != kind {
			t.Fatalf("field %q kind = %v, want %v", field.Name, field.Type.Kind(), kind)
		}
	}
}

func TestModelsRegisterCapturesPrimaryKeyTag(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("User")
	for _, field := range meta.Fields {
		if field.Name == "ID" {
			if !field.PrimaryKey {
				t.Fatal("ID field PrimaryKey = false, want true")
			}
			if field.Editable {
				t.Fatal("ID field Editable = true, want false")
			}
			continue
		}
		if field.PrimaryKey {
			t.Fatalf("field %q PrimaryKey = true, want false", field.Name)
		}
		if !field.Editable {
			t.Fatalf("field %q Editable = false, want true", field.Name)
		}
	}
}

func TestModelsRegisterCapturesUniqueTag(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("User")
	for _, field := range meta.Fields {
		want := field.Name == "Email"
		if field.Unique != want {
			t.Fatalf("field %q Unique = %v, want %v", field.Name, field.Unique, want)
		}
	}
}

type IndexedPost struct {
	ID       int64  `tango:"pk"`
	Slug     string `tango:"unique,index"`
	Category string `tango:"index"`
	Title    string
}

func TestModelsRegisterCapturesIndexTag(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(IndexedPost{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("IndexedPost")
	for _, field := range meta.Fields {
		want := field.Name == "Slug" || field.Name == "Category"
		if field.Indexed != want {
			t.Fatalf("field %q Indexed = %v, want %v", field.Name, field.Indexed, want)
		}
	}
}

func TestModelsRegisterUniqueFieldCanAlsoBeIndexed(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(IndexedPost{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("IndexedPost")
	for _, field := range meta.Fields {
		if field.Name != "Slug" {
			continue
		}
		if !field.Unique || !field.Indexed {
			t.Fatalf("field %q Unique=%v Indexed=%v, want both true", field.Name, field.Unique, field.Indexed)
		}
	}
}

type AllKinds struct {
	ID        int64 `tango:"pk"`
	Name      string
	Active    bool
	Count     int
	Ratio     float64
	CreatedAt time.Time
}

func TestModelsRegisterSupportsAllBasicKinds(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(AllKinds{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("AllKinds")
	want := map[string]reflect.Type{
		"ID":        reflect.TypeOf(int64(0)),
		"Name":      reflect.TypeOf(""),
		"Active":    reflect.TypeOf(false),
		"Count":     reflect.TypeOf(0),
		"Ratio":     reflect.TypeOf(float64(0)),
		"CreatedAt": reflect.TypeOf(time.Time{}),
	}

	if len(meta.Fields) != len(want) {
		t.Fatalf("Fields = %v, want %d entries", meta.Fields, len(want))
	}
	for _, field := range meta.Fields {
		wantType, known := want[field.Name]
		if !known {
			t.Fatalf("unexpected field %q", field.Name)
		}
		if field.Type != wantType {
			t.Fatalf("field %q type = %v, want %v", field.Name, field.Type, wantType)
		}
	}
}

type Account struct {
	ID int64 `tango:"pk"`
}

func TestModelsRegisterDuplicateDoesNotOverwriteOriginal(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Account{}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	original, _ := registry.Get("Account")

	type Account struct {
		ID   int64 `tango:"pk"`
		Note string
	}
	if err := registry.Register(Account{}); err == nil {
		t.Fatal("second Register returned nil error, want non-nil")
	}

	got, ok := registry.Get("Account")
	if !ok {
		t.Fatal("Get(\"Account\") returned false after failed duplicate Register")
	}
	if len(got.Fields) != len(original.Fields) {
		t.Fatalf("Fields changed after failed duplicate Register: got %v, want %v", got.Fields, original.Fields)
	}
}

type NoPrimaryKey struct {
	Name string
}

type MultiplePrimaryKeys struct {
	ID   int64  `tango:"pk"`
	UUID string `tango:"pk"`
}

type UntaggedID struct {
	ID   int64
	Name string
}

// TestModelsRegisterStructLevelValidationFailures merges
// TestModelsRegisterNoPrimaryKeyFails, TestModelsRegisterMultiplePrimaryKeysFails,
// TestModelsRegisterFieldNamedIDWithoutTagStillFails and
// TestModelsRegisterDuplicateNameFails into one table.
func TestModelsRegisterStructLevelValidationFailures(t *testing.T) {
	cases := []struct {
		name    string
		run     func(registry *Registry) error
		wantErr error
	}{
		{
			name: "struct with no pk field fails",
			run: func(registry *Registry) error {
				return registry.Register(NoPrimaryKey{})
			},
			wantErr: ErrNoPrimaryKey,
		},
		{
			name: "struct with two pk fields fails",
			run: func(registry *Registry) error {
				return registry.Register(MultiplePrimaryKeys{})
			},
			wantErr: ErrMultiplePrimaryKeys,
		},
		{
			name: "field named ID without pk tag still fails (no name-based auto-detection)",
			run: func(registry *Registry) error {
				return registry.Register(UntaggedID{})
			},
			wantErr: ErrNoPrimaryKey,
		},
		{
			name: "registering a second model with the same derived name fails",
			run: func(registry *Registry) error {
				if err := registry.Register(Account{}); err != nil {
					return err
				}
				type Account struct {
					ID int64 `tango:"pk"`
				}
				return registry.Register(Account{})
			},
			wantErr: ErrDuplicateModel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			err := tc.run(registry)
			if err == nil {
				t.Fatalf("case %q: Register returned nil error, want it to wrap %v", tc.name, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("case %q: error = %v, want it to wrap %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestModelsRegisterFailedValidationDoesNotRegisterModel(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(NoPrimaryKey{}); err == nil {
		t.Fatal("Register returned nil error, want non-nil")
	}
	if _, ok := registry.Get("NoPrimaryKey"); ok {
		t.Fatal("Get returned true for a model that failed validation")
	}
}

type SliceField struct {
	ID   int64 `tango:"pk"`
	Tags []string
}

type MapField struct {
	ID     int64 `tango:"pk"`
	Labels map[string]string
}

type PointerField struct {
	ID     int64 `tango:"pk"`
	Parent *PointerField
}

type InterfaceField struct {
	ID      int64 `tango:"pk"`
	Payload any
}

type OtherStruct struct {
	Value string
}

type NonTimeStructField struct {
	ID    int64 `tango:"pk"`
	Other OtherStruct
}

type TimeField struct {
	ID        int64 `tango:"pk"`
	CreatedAt time.Time
}

type EmbeddedField struct {
	ID int64 `tango:"pk"`
	OtherStruct
}

// TestModelsRegisterFieldKindValidation merges TestModelsRegisterSliceFieldFails,
// TestModelsRegisterMapFieldFails, TestModelsRegisterPointerFieldFails,
// TestModelsRegisterInterfaceFieldFails, TestModelsRegisterNonTimeStructFieldFails,
// TestModelsRegisterEmbeddedStructFieldFails and TestModelsRegisterTimeTimeFieldSucceeds
// into one table.
func TestModelsRegisterFieldKindValidation(t *testing.T) {
	cases := []struct {
		name      string
		newStruct func() any
		wantErr   error // nil means Register must succeed
	}{
		{
			name:      "slice field is unsupported",
			newStruct: func() any { return SliceField{} },
			wantErr:   ErrUnsupportedField,
		},
		{
			name:      "map field is unsupported",
			newStruct: func() any { return MapField{} },
			wantErr:   ErrUnsupportedField,
		},
		{
			name:      "pointer field is unsupported",
			newStruct: func() any { return PointerField{} },
			wantErr:   ErrUnsupportedField,
		},
		{
			name:      "interface field is unsupported",
			newStruct: func() any { return InterfaceField{} },
			wantErr:   ErrUnsupportedField,
		},
		{
			name:      "non-time.Time struct field is unsupported",
			newStruct: func() any { return NonTimeStructField{} },
			wantErr:   ErrUnsupportedField,
		},
		{
			name:      "embedded struct field is rejected outright",
			newStruct: func() any { return EmbeddedField{} },
			wantErr:   ErrEmbeddedField,
		},
		{
			name:      "time.Time field succeeds",
			newStruct: func() any { return TimeField{} },
			wantErr:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(tc.newStruct())

			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("case %q: Register returned error: %v, want nil", tc.name, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("case %q: error = %v, want it to wrap %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestModelsRegisterFailedFieldValidationDoesNotRegisterModel(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(SliceField{}); err == nil {
		t.Fatal("Register returned nil error, want non-nil")
	}
	if _, ok := registry.Get("SliceField"); ok {
		t.Fatal("Get returned true for a model that failed field validation")
	}
}

type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID       int64 `tango:"pk"`
	Title    string
	AuthorID int64 `tango:"fk=Author,index"`
}

func TestModelsRegisterCapturesForeignKeyTagAlongsideBareFlags(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Post{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("Post")
	var field FieldMeta
	for _, f := range meta.Fields {
		if f.Name == "AuthorID" {
			field = f
		}
	}
	if field.ForeignKey != "Author" {
		t.Fatalf("ForeignKey = %q, want %q", field.ForeignKey, "Author")
	}
	if !field.Indexed {
		t.Fatal("Indexed = false, want true — bare flags alongside fk= must still parse")
	}
}

func TestModelsRegisterNonForeignKeyFieldLeavesForeignKeyEmpty(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(User{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	meta, _ := registry.Get("User")
	for _, f := range meta.Fields {
		if f.ForeignKey != "" {
			t.Fatalf("field %q ForeignKey = %q, want empty", f.Name, f.ForeignKey)
		}
	}
}

// TestValidateForeignKeys merges TestValidateForeignKeysPassesWhenTargetIsRegistered,
// TestValidateForeignKeysIgnoresRegistrationOrder and
// TestValidateForeignKeysFailsOnDanglingTarget into one table.
func TestValidateForeignKeys(t *testing.T) {
	cases := []struct {
		name            string
		register        []any
		wantErr         error // nil means ValidateForeignKeys must succeed
		wantErrContains []string
	}{
		{
			name:     "passes when foreign key target is registered",
			register: []any{Author{}, Post{}},
			wantErr:  nil,
		},
		{
			// Post references Author but is registered first — ValidateForeignKeys
			// runs once after every app has registered, so order must not matter.
			name:     "ignores registration order (InstalledApps-style order should not matter)",
			register: []any{Post{}, Author{}},
			wantErr:  nil,
		},
		{
			name:            "fails on dangling foreign key target",
			register:        []any{Post{}}, // Author never registered.
			wantErr:         ErrUnknownForeignKeyTarget,
			wantErrContains: []string{"Post.AuthorID", "Author"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, model := range tc.register {
				if err := registry.Register(model); err != nil {
					t.Fatalf("case %q: Register(%T) returned error: %v", tc.name, model, err)
				}
			}

			err := registry.ValidateForeignKeys()

			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("case %q: ValidateForeignKeys returned error: %v, want nil", tc.name, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("case %q: error = %v, want it to wrap %v", tc.name, err, tc.wantErr)
			}
			for _, substr := range tc.wantErrContains {
				if !strings.Contains(err.Error(), substr) {
					t.Errorf("case %q: error = %q, want it to contain %q", tc.name, err.Error(), substr)
				}
			}
		})
	}
}

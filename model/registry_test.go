package model

import (
	"errors"
	"reflect"
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

func TestModelsRegisterDuplicateNameFails(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Account{}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	type Account struct {
		ID int64 `tango:"pk"`
	}

	err := registry.Register(Account{})
	if err == nil {
		t.Fatal("second Register returned nil error for duplicate name, want non-nil")
	}
	if !errors.Is(err, ErrDuplicateModel) {
		t.Fatalf("error = %v, want it to wrap ErrDuplicateModel", err)
	}
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

func TestModelsRegisterNoPrimaryKeyFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(NoPrimaryKey{})
	if err == nil {
		t.Fatal("Register returned nil error for a struct with no pk field, want non-nil")
	}
	if !errors.Is(err, ErrNoPrimaryKey) {
		t.Fatalf("error = %v, want it to wrap ErrNoPrimaryKey", err)
	}
}

type MultiplePrimaryKeys struct {
	ID   int64  `tango:"pk"`
	UUID string `tango:"pk"`
}

func TestModelsRegisterMultiplePrimaryKeysFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(MultiplePrimaryKeys{})
	if err == nil {
		t.Fatal("Register returned nil error for a struct with two pk fields, want non-nil")
	}
	if !errors.Is(err, ErrMultiplePrimaryKeys) {
		t.Fatalf("error = %v, want it to wrap ErrMultiplePrimaryKeys", err)
	}
}

type UntaggedID struct {
	ID   int64
	Name string
}

func TestModelsRegisterFieldNamedIDWithoutTagStillFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(UntaggedID{})
	if !errors.Is(err, ErrNoPrimaryKey) {
		t.Fatalf("error = %v, want it to wrap ErrNoPrimaryKey (no name-based auto-detection)", err)
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

func TestModelsRegisterSliceFieldFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(SliceField{})
	if !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("error = %v, want it to wrap ErrUnsupportedField", err)
	}
}

type MapField struct {
	ID     int64 `tango:"pk"`
	Labels map[string]string
}

func TestModelsRegisterMapFieldFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(MapField{})
	if !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("error = %v, want it to wrap ErrUnsupportedField", err)
	}
}

type PointerField struct {
	ID     int64 `tango:"pk"`
	Parent *PointerField
}

func TestModelsRegisterPointerFieldFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(PointerField{})
	if !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("error = %v, want it to wrap ErrUnsupportedField", err)
	}
}

type InterfaceField struct {
	ID      int64 `tango:"pk"`
	Payload any
}

func TestModelsRegisterInterfaceFieldFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(InterfaceField{})
	if !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("error = %v, want it to wrap ErrUnsupportedField", err)
	}
}

type OtherStruct struct {
	Value string
}

type NonTimeStructField struct {
	ID    int64 `tango:"pk"`
	Other OtherStruct
}

func TestModelsRegisterNonTimeStructFieldFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(NonTimeStructField{})
	if !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("error = %v, want it to wrap ErrUnsupportedField", err)
	}
}

type TimeField struct {
	ID        int64 `tango:"pk"`
	CreatedAt time.Time
}

func TestModelsRegisterTimeTimeFieldSucceeds(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(TimeField{}); err != nil {
		t.Fatalf("Register returned error for a time.Time field: %v", err)
	}
}

type EmbeddedField struct {
	ID int64 `tango:"pk"`
	OtherStruct
}

func TestModelsRegisterEmbeddedStructFieldFails(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(EmbeddedField{})
	if !errors.Is(err, ErrEmbeddedField) {
		t.Fatalf("error = %v, want it to wrap ErrEmbeddedField", err)
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

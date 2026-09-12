package model

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// ErrDuplicateModel is returned by Registry.Register when a model with the
// same derived Name has already been registered.
var ErrDuplicateModel = errors.New("tango: duplicate model name")

// ErrNoPrimaryKey is returned by Registry.Register when a struct has no
// field tagged tango:"pk".
var ErrNoPrimaryKey = errors.New("tango: model has no primary key field")

// ErrMultiplePrimaryKeys is returned by Registry.Register when a struct has
// more than one field tagged tango:"pk".
var ErrMultiplePrimaryKeys = errors.New("tango: model has multiple primary key fields")

// ErrUnsupportedField is returned by Registry.Register when a field's kind
// isn't supported by the metadata layer.
var ErrUnsupportedField = errors.New("tango: unsupported field type")

// ErrEmbeddedField is returned by Registry.Register when a struct has an
// embedded (anonymous) field. Embedded fields are rejected outright, not
// flattened, in v0.1.
var ErrEmbeddedField = errors.New("tango: embedded fields are not supported")

var timeType = reflect.TypeOf(time.Time{})

// ModelMeta describes a registered Go struct model.
type ModelMeta struct {
	Name   string
	App    string
	Type   reflect.Type
	Fields []FieldMeta
}

// FieldMeta describes an exported Go struct field.
type FieldMeta struct {
	Name       string
	Type       reflect.Type
	PrimaryKey bool
	Unique     bool
	Indexed    bool
	Editable   bool
}

// Registry stores model metadata by Go type name.
type Registry struct {
	models     map[string]ModelMeta
	currentApp string
}

// NewRegistry returns an empty, ready-to-use model Registry.
func NewRegistry() *Registry {
	return &Registry{
		models: make(map[string]ModelMeta),
	}
}

// SetCurrentApp records the name of the App currently registering models,
// populating ModelMeta.App for every model Register call that follows,
// until the next SetCurrentApp call. It is intended to be called by
// tango.Registry.RunRegistration immediately before invoking each app's
// Register callback, not by app authors directly.
func (r *Registry) SetCurrentApp(name string) {
	r.currentApp = name
}

// Register introspects a struct value and stores its model metadata. It
// returns an error, and registers nothing, if the struct's derived name is
// already registered, if it has zero or multiple primary key fields, if it
// has an embedded field, or if any field's kind is unsupported.
func (r *Registry) Register(value any) error {
	modelType := reflect.TypeOf(value)
	if modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}

	name := modelType.Name()
	if _, exists := r.models[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateModel, name)
	}

	meta := ModelMeta{
		Name: name,
		App:  r.currentApp,
		Type: modelType,
	}

	var primaryKeyFields []string

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if !field.IsExported() {
			continue
		}

		if field.Anonymous {
			return fmt.Errorf("%w: field %q", ErrEmbeddedField, field.Name)
		}

		if !supportedFieldType(field.Type) {
			return fmt.Errorf("%w: field %q has type %s", ErrUnsupportedField, field.Name, field.Type)
		}

		tag := field.Tag.Get("tango")
		primaryKey := hasTagOption(tag, "pk")
		if primaryKey {
			primaryKeyFields = append(primaryKeyFields, field.Name)
		}

		meta.Fields = append(meta.Fields, FieldMeta{
			Name:       field.Name,
			Type:       field.Type,
			PrimaryKey: primaryKey,
			Unique:     hasTagOption(tag, "unique"),
			Indexed:    hasTagOption(tag, "index"),
			Editable:   !primaryKey,
		})
	}

	switch len(primaryKeyFields) {
	case 0:
		return fmt.Errorf("%w: %q", ErrNoPrimaryKey, name)
	case 1:
		// exactly one primary key, as required
	default:
		return fmt.Errorf("%w: %q has fields %s", ErrMultiplePrimaryKeys, name, strings.Join(primaryKeyFields, ", "))
	}

	r.models[meta.Name] = meta

	return nil
}

// Get returns model metadata by Go type name.
func (r *Registry) Get(name string) (ModelMeta, bool) {
	meta, ok := r.models[name]
	return meta, ok
}

// All returns every registered model's metadata, sorted by Name for a
// deterministic order.
func (r *Registry) All() []ModelMeta {
	all := make([]ModelMeta, 0, len(r.models))
	for _, meta := range r.models {
		all = append(all, meta)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}

// supportedFieldType reports whether t is a kind the metadata layer can
// describe in v0.1: string, bool, any integer/float kind, or time.Time as
// the sole named-struct exception.
func supportedFieldType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Struct:
		return t == timeType
	default:
		return false
	}
}

func hasTagOption(tag string, option string) bool {
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == option {
			return true
		}
	}
	return false
}

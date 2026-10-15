package model

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
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
// flattened.
var ErrEmbeddedField = errors.New("tango: embedded fields are not supported")

// ErrUnknownForeignKeyTarget is returned by ValidateForeignKeys when a
// field's tango:"fk=X" tag names a model X that no installed app ever
// registered.
var ErrUnknownForeignKeyTarget = errors.New("tango: foreign key targets an unregistered model")

// ErrInvalidFieldTag is returned by Registry.Register when a field's tango
// tag declares a string length wrongly: varchar without a positive length, a
// length beyond MaxVarcharLength, varchar together with text, a repeated
// varchar or text, or either on a field that is not a string. The error
// names the model and field.
var ErrInvalidFieldTag = errors.New("tango: invalid field tag")

// MaxVarcharLength is the largest length tango:"varchar=n" accepts: the
// largest VARCHAR(n) PostgreSQL allows, so tanGO adds no ceiling of its own.
const MaxVarcharLength = 10485760

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
	ForeignKey string // target model name from tango:"fk=<Name>"; "" if not a foreign key
	// MaxLength is the most runes a bounded string may hold, from
	// tango:"varchar=n"; 0 means unbounded (a bare string or tango:"text").
	MaxLength int
	// JSON is true for a string field declared tango:"json": it holds JSON
	// text, which Store.Create and Store.Update check is valid. The column is
	// ordinary text.
	JSON bool
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

		fk, _ := tagValue(tag, "fk")

		maxLength, err := lengthTag(tag, field.Type)
		if err != nil {
			return fmt.Errorf("%w: %s.%s: %v", ErrInvalidFieldTag, name, field.Name, err)
		}

		isJSON, err := jsonTag(tag, field.Type)
		if err != nil {
			return fmt.Errorf("%w: %s.%s: %v", ErrInvalidFieldTag, name, field.Name, err)
		}

		indexed, err := indexedTag(tag, fk, hasTagOption(tag, "unique"))
		if err != nil {
			return fmt.Errorf("%w: %s.%s: %v", ErrInvalidFieldTag, name, field.Name, err)
		}

		meta.Fields = append(meta.Fields, FieldMeta{
			Name:       field.Name,
			Type:       field.Type,
			PrimaryKey: primaryKey,
			Unique:     hasTagOption(tag, "unique"),
			Indexed:    indexed,
			Editable:   !primaryKey,
			ForeignKey: fk,
			MaxLength:  maxLength,
			JSON:       isJSON,
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
// describe: string, bool, any integer/float kind, or time.Time as
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

// lengthTag reads tango:"varchar=n" and tango:"text" from tag for a field of
// type t: the declared maximum length, or 0 for a bare string or an explicit
// text. It rejects every malformed declaration.
func lengthTag(tag string, t reflect.Type) (int, error) {
	var varchars, texts []string
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		name, _, hasValue := strings.Cut(part, "=")
		switch strings.TrimSpace(name) {
		case "varchar":
			varchars = append(varchars, part)
		case "text":
			if hasValue {
				return 0, fmt.Errorf("text takes no value, got %q", part)
			}
			texts = append(texts, part)
		}
	}
	if len(varchars) == 0 && len(texts) == 0 {
		return 0, nil
	}
	if t.Kind() != reflect.String {
		return 0, fmt.Errorf("varchar and text apply to string fields, not %s", t)
	}
	switch {
	case len(varchars) > 1:
		return 0, fmt.Errorf("varchar is declared more than once")
	case len(texts) > 1:
		return 0, fmt.Errorf("text is declared more than once")
	case len(varchars) == 1 && len(texts) == 1:
		return 0, fmt.Errorf("varchar and text cannot be combined")
	case len(texts) == 1:
		return 0, nil
	}
	value, ok := tagValue(tag, "varchar")
	if !ok || value == "" {
		return 0, fmt.Errorf("varchar needs a length, as varchar=200")
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || strings.Trim(value, "0123456789") != "" {
		return 0, fmt.Errorf("varchar needs a positive whole number, got %q", value)
	}
	if n > MaxVarcharLength {
		return 0, fmt.Errorf("varchar=%d is above the largest length, %d", n, MaxVarcharLength)
	}
	return n, nil
}

// jsonTag reads tango:"json" from tag for a field of type t. It rejects a
// json declaration that is repeated, carries a value, sits on a non-string
// field, or is combined with a tag that would make the field compare or
// bound its text as something other than JSON text.
func jsonTag(tag string, t reflect.Type) (bool, error) {
	declared := 0
	for _, part := range strings.Split(tag, ",") {
		name, _, hasValue := strings.Cut(strings.TrimSpace(part), "=")
		if strings.TrimSpace(name) != "json" {
			continue
		}
		if hasValue {
			return false, fmt.Errorf("json takes no value, got %q", strings.TrimSpace(part))
		}
		declared++
	}
	if declared == 0 {
		return false, nil
	}
	if declared > 1 {
		return false, fmt.Errorf("json is declared more than once")
	}
	if t.Kind() != reflect.String {
		return false, fmt.Errorf("json applies to string fields, not %s", t)
	}
	for _, other := range []string{"varchar", "text", "pk", "unique", "index"} {
		if _, hasValue := tagValue(tag, other); hasValue || hasTagOption(tag, other) {
			return false, fmt.Errorf("json cannot be combined with %s", other)
		}
	}
	if _, isFK := tagValue(tag, "fk"); isFK {
		return false, fmt.Errorf("json cannot be combined with fk")
	}
	return true, nil
}

func hasTagOption(tag string, option string) bool {
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == option {
			return true
		}
	}
	return false
}

// indexedTag reports whether a field's column is indexed: with the "index"
// tag, or, for a foreign key, by default (as Django does), unless the field
// says "noindex" or is "unique" (a unique constraint already indexes it).
func indexedTag(tag, foreignKey string, unique bool) (bool, error) {
	index, noindex := hasTagOption(tag, "index"), hasTagOption(tag, "noindex")
	switch {
	case index && noindex:
		return false, fmt.Errorf("index and noindex cannot be combined")
	case noindex && foreignKey == "":
		return false, fmt.Errorf("noindex applies only to a foreign key (fk=Model), which is indexed by default")
	case noindex:
		return false, nil
	case index:
		return true, nil
	}
	return foreignKey != "" && !unique, nil
}

// tagValue returns the value of a "key=value" pair in a comma-separated
// tango tag (e.g. tagValue(`fk=Author,index`, "fk") returns ("Author",
// true)), alongside the existing bare-flag options like "pk"/"unique". A
// bare flag with the given name (no "=") does not count as a value.
func tagValue(tag string, key string) (string, bool) {
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

// ValidateForeignKeys checks that every registered field's tango:"fk=X" tag
// names a model X that is also registered in this same Registry. It is
// intended to run once, after every installed app has finished registering
// (e.g. from tango.Check), not per-model at Register time — so InstalledApps
// order never constrains which app may declare a foreign key relative to
// the app that registers its target.
func (r *Registry) ValidateForeignKeys() error {
	for _, meta := range r.All() {
		for _, field := range meta.Fields {
			if field.ForeignKey == "" {
				continue
			}
			if _, exists := r.models[field.ForeignKey]; !exists {
				return fmt.Errorf("%w: %s.%s references %q", ErrUnknownForeignKeyTarget, meta.Name, field.Name, field.ForeignKey)
			}
		}
	}
	return nil
}

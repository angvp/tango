package openapi

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	timeType      = reflect.TypeOf(time.Time{})
	rawJSONType   = reflect.TypeOf(json.RawMessage(nil))
	textMarshaler = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// schemas turns Go types into JSON Schema, collecting named structs into
// components/schemas so a type used twice is described once.
type schemas struct {
	components map[string]map[string]any
	names      map[reflect.Type]string
}

func newSchemas() *schemas {
	return &schemas{components: map[string]map[string]any{}, names: map[reflect.Type]string{}}
}

// of returns the schema for a sample value's type.
func (s *schemas) of(sample any) (map[string]any, error) {
	if sample == nil {
		return nil, fmt.Errorf("nil sample value: pass a zero value such as Pet{}")
	}
	t := reflect.TypeOf(sample)
	for t.Kind() == reflect.Pointer {
		t = t.Elem() // a sample pointer only shows the type; the body itself is not null
	}
	return s.forType(t)
}

func (s *schemas) forType(t reflect.Type) (map[string]any, error) {
	nullable := false
	for t.Kind() == reflect.Pointer {
		t, nullable = t.Elem(), true
	}
	schema, err := s.forNonPointer(t)
	if err != nil {
		return nil, err
	}
	if !nullable {
		return schema, nil
	}
	if ref, isRef := schema["$ref"]; isRef {
		return map[string]any{"oneOf": []any{map[string]any{"$ref": ref}, map[string]any{"type": "null"}}}, nil
	}
	if kind, ok := schema["type"].(string); ok {
		schema["type"] = []any{kind, "null"}
	}
	return schema, nil
}

func (s *schemas) forNonPointer(t reflect.Type) (map[string]any, error) {
	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}, nil
	case t == rawJSONType:
		return map[string]any{}, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16:
		return map[string]any{"type": "integer", "format": "int32"}, nil
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}, nil
	case reflect.Float32:
		return map[string]any{"type": "number", "format": "float"}, nil
	case reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Interface:
		return map[string]any{}, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}, nil
		}
		return s.array(t)
	case reflect.Array:
		return s.array(t)
	case reflect.Map:
		return s.mapOf(t)
	case reflect.Struct:
		return s.structRef(t)
	}
	return nil, fmt.Errorf("%s cannot be described: %s values have no JSON form", t, t.Kind())
}

func (s *schemas) array(t reflect.Type) (map[string]any, error) {
	items, err := s.forType(t.Elem())
	if err != nil {
		return nil, err
	}
	return map[string]any{"type": "array", "items": items}, nil
}

func (s *schemas) mapOf(t reflect.Type) (map[string]any, error) {
	switch t.Key().Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		if !t.Key().Implements(textMarshaler) {
			return nil, fmt.Errorf("%s cannot be described: JSON object keys must be strings or integers", t)
		}
	}
	values, err := s.forType(t.Elem())
	if err != nil {
		return nil, err
	}
	return map[string]any{"type": "object", "additionalProperties": values}, nil
}

// structRef describes a named struct once under components/schemas and returns
// a reference to it. An unnamed struct is described inline.
func (s *schemas) structRef(t reflect.Type) (map[string]any, error) {
	if t.Name() == "" {
		return s.structBody(t)
	}
	if name, seen := s.names[t]; seen {
		return map[string]any{"$ref": "#/components/schemas/" + name}, nil
	}
	name := s.componentName(t)
	s.names[t] = name
	s.components[name] = nil // reserve the name so a recursive type finds itself
	body, err := s.structBody(t)
	if err != nil {
		delete(s.names, t)
		delete(s.components, name)
		return nil, err
	}
	s.components[name] = body
	return map[string]any{"$ref": "#/components/schemas/" + name}, nil
}

// componentName is the type's name, qualified by its package when another type
// already took that name.
func (s *schemas) componentName(t reflect.Type) string {
	name := t.Name()
	if _, taken := s.components[name]; !taken {
		return name
	}
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	qualified := identifier(pkg) + "_" + name
	for n := 2; ; n++ {
		if _, taken := s.components[qualified]; !taken {
			return qualified
		}
		qualified = fmt.Sprintf("%s_%s_%d", identifier(pkg), name, n)
	}
}

func (s *schemas) structBody(t reflect.Type) (map[string]any, error) {
	properties := map[string]any{}
	var required []string
	if err := s.collectFields(t, properties, &required); err != nil {
		return nil, err
	}
	body := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		body["required"] = required
	}
	return body, nil
}

func (s *schemas) collectFields(t reflect.Type, properties map[string]any, required *[]string) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() && !field.Anonymous {
			continue
		}
		name, options, skip := jsonName(field)
		if skip {
			continue
		}
		if field.Anonymous && name == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct && embedded != timeType {
				if err := s.collectFields(embedded, properties, required); err != nil {
					return err
				}
				continue
			}
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		schema, err := s.forType(field.Type)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
		applyModelTag(schema, field)
		properties[name] = schema
		if field.Type.Kind() != reflect.Pointer && !options["omitempty"] && !options["omitzero"] {
			*required = append(*required, name)
		}
	}
	return nil
}

// jsonName reads a field's json tag: its JSON name ("" when the Go name is
// used), its options, and whether the field is left out of JSON.
func jsonName(field reflect.StructField) (string, map[string]bool, bool) {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", map[string]bool{}, false
	}
	if tag == "-" {
		return "", nil, true
	}
	name, rest, _ := strings.Cut(tag, ",")
	options := map[string]bool{}
	for _, option := range strings.Split(rest, ",") {
		options[option] = true
	}
	return name, options, false
}

// applyModelTag carries what a model field's tango tag says about its size
// into the schema: tango:"varchar=n" bounds a string to n characters.
func applyModelTag(schema map[string]any, field reflect.StructField) {
	if _, isRef := schema["$ref"]; isRef {
		return
	}
	for _, option := range strings.Split(field.Tag.Get("tango"), ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(option), "varchar="); ok {
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				schema["maxLength"] = n
			}
		}
	}
}

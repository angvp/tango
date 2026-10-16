package openapi

import (
	"fmt"
	"reflect"
	"strings"
)

// queryParameters describes the fields of a struct as query parameters. A field
// is named by its `query:"name"` tag, or its lower-cased Go name, and is
// required when the tag says `query:"name,required"`. A field of `query:"-"`
// is left out.
func queryParameters(sample any, known *schemas) ([]map[string]any, error) {
	t := reflect.TypeOf(sample)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s must be a struct whose fields are the query parameters", t)
	}
	var out []map[string]any
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("query")
		if tag == "-" {
			continue
		}
		name, rest, _ := strings.Cut(tag, ",")
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		element := field.Type
		for element.Kind() == reflect.Pointer {
			element = element.Elem()
		}
		if element.Kind() == reflect.Struct && element != timeType {
			return nil, fmt.Errorf("%s.%s: a query parameter cannot be a struct", t.Name(), field.Name)
		}
		schema, err := known.forType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
		out = append(out, map[string]any{
			"name": name, "in": "query", "required": strings.Contains(","+rest+",", ",required,"), "schema": schema,
		})
	}
	return out, nil
}

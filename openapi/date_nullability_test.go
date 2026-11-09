package openapi_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/model"
)

// dateBody places a Date in every body position that carries one: a value
// field, an optional pointer, an array element, and a map value.
type dateBody struct {
	ID     int64                 `json:"id" tango:"pk"`
	Day    model.Date            `json:"day"`
	Next   *model.Date           `json:"next,omitempty"`
	Days   []model.Date          `json:"days"`
	ByName map[string]model.Date `json:"byName"`
}

// schemaTypeSet returns the JSON types a schema allows, for "type" written as a
// string or as an array.
func schemaTypeSet(t *testing.T, schema map[string]any) []string {
	t.Helper()
	switch typ := schema["type"].(type) {
	case string:
		return []string{typ}
	case []any:
		var out []string
		for _, v := range typ {
			out = append(out, v.(string))
		}
		return out
	}
	t.Fatalf("schema has no type: %v", schema)
	return nil
}

func allows(types []string, want string) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

// validateWire checks one JSON value against a schema that uses only the type
// set and format "date". It is not a JSON Schema validator. It proves that the
// value's JSON type is in the schema's type set and, for a string, that the
// value is an exact calendar date (model.ParseDate) when the format is "date".
func validateWire(types []string, format string, instance any) error {
	switch v := instance.(type) {
	case nil:
		if !allows(types, "null") {
			return fmt.Errorf("null is not an allowed value (types %v)", types)
		}
		return nil
	case string:
		if !allows(types, "string") {
			return fmt.Errorf("a string is not an allowed value (types %v)", types)
		}
		if format == "date" {
			if _, err := model.ParseDate(v); err != nil {
				return fmt.Errorf("%q is not a valid date: %w", v, err)
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported instance %T", instance)
}

// wireOf marshals v and decodes it back into the generic JSON form a client
// would read, so the test checks the bytes that go on the wire.
func wireOf(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDateBodyPositionsAllowTheWireValues(t *testing.T) {
	doc := documentOf(t, tango.Path("GET", "/{id}", view, tango.Doc(tango.RouteDoc{Responses: map[int]any{200: dateBody{}}})))
	properties := at(t, doc, "components", "schemas", "dateBody", "properties").(map[string]any)

	day := properties["day"].(map[string]any)
	next := properties["next"].(map[string]any)
	days := properties["days"].(map[string]any)["items"].(map[string]any)
	byName := properties["byName"].(map[string]any)["additionalProperties"].(map[string]any)

	cases := []struct {
		name   string
		schema map[string]any
		value  any
	}{
		{"day, zero", day, wireOf(t, dateBody{})["day"]},
		{"day, set", day, wireOf(t, dateBody{Day: model.MustDate(2026, 1, 2)})["day"]},
		{"next, set", next, wireOf(t, dateBody{Next: ptrDate(model.MustDate(2026, 1, 2))})["next"]},
		{"days element, zero", days, wireOf(t, dateBody{Days: []model.Date{{}}})["days"].([]any)[0]},
		{"days element, set", days, wireOf(t, dateBody{Days: []model.Date{model.MustDate(2026, 1, 2)}})["days"].([]any)[0]},
		{"byName value, zero", byName, wireOf(t, dateBody{ByName: map[string]model.Date{"x": {}}})["byName"].(map[string]any)["x"]},
		{"byName value, set", byName, wireOf(t, dateBody{ByName: map[string]model.Date{"x": model.MustDate(2026, 1, 2)}})["byName"].(map[string]any)["x"]},
	}
	for _, c := range cases {
		format, _ := c.schema["format"].(string)
		if err := validateWire(schemaTypeSet(t, c.schema), format, c.value); err != nil {
			t.Errorf("%s: wire value %v does not validate against %v: %v", c.name, c.value, c.schema, err)
		}
	}
	if err := validateWire(schemaTypeSet(t, day), "date", "2026-1-2"); err == nil {
		t.Error("the helper accepted a date that is not exact YYYY-MM-DD; the check proves nothing")
	}
}

func TestDateQueryParameterIsNotNullable(t *testing.T) {
	doc := documentOf(t, tango.Path("GET", "/", view, tango.Doc(tango.RouteDoc{Query: dateFilter{}})))
	params := at(t, doc, "paths", "/pets/", "get", "parameters").([]any)
	schema := params[0].(map[string]any)["schema"].(map[string]any)
	types := schemaTypeSet(t, schema)
	if allows(types, "null") {
		t.Errorf("a query parameter allows null: types %v; a query value is never an unset date", types)
	}
	if err := validateWire(types, "date", nil); err == nil {
		t.Error("null validates against the query parameter schema")
	}
	if err := validateWire(types, "date", "2026-01-02"); err != nil {
		t.Errorf("an exact date is rejected by the query parameter schema: %v", err)
	}
}

// The request decoder reads a body with json.Decoder.Decode, which calls
// Date.UnmarshalJSON. null must decode to the zero Date, so the request schema
// allows null.
func TestDateRequestBodyAcceptsNull(t *testing.T) {
	var got dateBody
	if err := json.Unmarshal([]byte(`{"day":null,"days":[null,"2026-01-02"]}`), &got); err != nil {
		t.Fatalf("a request with null dates is rejected: %v", err)
	}
	if !got.Day.IsZero() || got.Days[0] != (model.Date{}) || got.Days[1] != model.MustDate(2026, 1, 2) {
		t.Errorf("decoded %+v", got)
	}
}

func ptrDate(d model.Date) *model.Date { return &d }

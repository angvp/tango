package openapi_test

import (
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/model"
)

type datedBody struct {
	ID    int64      `json:"id" tango:"pk"`
	Day   model.Date `json:"day"`
	Born  time.Time  `json:"born"`
	Other model.Date `json:"other,omitempty"`
}

type dateFilter struct {
	Day model.Date `query:"day"`
}

func TestDateIsAStringDateAndTimeStaysDateTime(t *testing.T) {
	doc := documentOf(t, tango.Path("GET", "/{id}", view, tango.Doc(tango.RouteDoc{Responses: map[int]any{200: datedBody{}}})))
	schemas := at(t, doc, "components", "schemas").(map[string]any)
	if _, ok := schemas["Date"]; ok {
		t.Error("model.Date was described as a component object")
	}
	body := schemas["datedBody"].(map[string]any)
	properties := body["properties"].(map[string]any)
	equal(t, properties["day"], map[string]any{"type": "string", "format": "date"})
	equal(t, properties["other"], map[string]any{"type": "string", "format": "date"})
	equal(t, properties["born"], map[string]any{"type": "string", "format": "date-time"})
}

func TestDateQueryParameterIsAStringDate(t *testing.T) {
	doc := documentOf(t, tango.Path("GET", "/", view, tango.Doc(tango.RouteDoc{Query: dateFilter{}})))
	params, ok := at(t, doc, "paths", "/pets/", "get", "parameters").([]any)
	if !ok || len(params) != 1 {
		t.Fatalf("parameters = %v, want the day parameter", params)
	}
	equal(t, params[0], map[string]any{
		"name": "day", "in": "query", "required": false,
		"schema": map[string]any{"type": "string", "format": "date"},
	})
}

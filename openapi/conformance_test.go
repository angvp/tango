package openapi_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/openapi"
)

// checkDocument verifies the parts of the OpenAPI 3.1 specification the
// package emits: required top-level and info members, a path item per path
// with only the specification's method keys, a responses object on every
// operation, unique operationIds, and every {placeholder} of a path declared
// as a required path parameter (and no other path parameter declared). It is a
// structural check, not a full JSON Schema validation, so the module carries
// no validator dependency.
func checkDocument(t *testing.T, raw []byte) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if v, _ := doc["openapi"].(string); !strings.HasPrefix(v, "3.1.") {
		t.Fatalf("openapi = %v, want 3.1.x", doc["openapi"])
	}
	info, _ := doc["info"].(map[string]any)
	if info["title"] == nil || info["version"] == nil {
		t.Fatalf("info needs title and version: %v", info)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths must be an object")
	}
	placeholder := regexp.MustCompile(`\{([^}]+)\}`)
	methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true}
	ids := map[string]bool{}
	for path, item := range paths {
		if !strings.HasPrefix(path, "/") {
			t.Errorf("path %q must start with /", path)
		}
		for method, opAny := range item.(map[string]any) {
			if !methods[method] {
				t.Errorf("%s: %q is not an operation key", path, method)
				continue
			}
			op := opAny.(map[string]any)
			if _, ok := op["responses"].(map[string]any); !ok {
				t.Errorf("%s %s: responses missing", method, path)
			}
			if id, _ := op["operationId"].(string); id == "" || ids[id] {
				t.Errorf("%s %s: operationId %q missing or duplicated", method, path, id)
			} else {
				ids[id] = true
			}
			declared := map[string]bool{}
			params, _ := op["parameters"].([]any)
			for _, p := range params {
				param := p.(map[string]any)
				if param["in"] == "path" {
					declared[param["name"].(string)] = true
					if param["required"] != true {
						t.Errorf("%s %s: path parameter %v must be required", method, path, param["name"])
					}
				}
			}
			for _, m := range placeholder.FindAllStringSubmatch(path, -1) {
				if !declared[m[1]] {
					t.Errorf("%s %s: {%s} is not declared", method, path, m[1])
				}
				delete(declared, m[1])
			}
			for extra := range declared {
				t.Errorf("%s %s: parameter %s is not in the path", method, path, extra)
			}
		}
	}
}

func TestDocumentConformsToTheSpecification(t *testing.T) {
	h := serve(t, openapi.Options{Title: "Shop", Servers: []string{"https://api.example.com"}}, shopRoutes)
	checkDocument(t, fetch(t, h, "/openapi.json").Body.Bytes())
}

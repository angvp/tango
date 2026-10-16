package openapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/angvp/tango"
)

// Version is the OpenAPI version of the documents this package writes.
const Version = "3.1.0"

var operationMethods = map[string]string{
	http.MethodGet:     "get",
	http.MethodPut:     "put",
	http.MethodPost:    "post",
	http.MethodDelete:  "delete",
	http.MethodOptions: "options",
	http.MethodHead:    "head",
	http.MethodPatch:   "patch",
	http.MethodTrace:   "trace",
}

// Document builds the OpenAPI document for routes. It is deterministic: the
// same routes and options give the same bytes once marshalled.
func Document(routes []tango.DescribedRoute, opts Options) (map[string]any, error) {
	opts = opts.withDefaults()
	paths := map[string]map[string]any{}
	usedIDs := map[string]bool{}
	tags := map[string]bool{}
	for _, route := range routes {
		if skip(route, opts) {
			continue
		}
		method, ok := operationMethods[strings.ToUpper(route.Method)]
		if !ok {
			continue
		}
		template, params, err := convertPattern(route.Pattern)
		if err != nil {
			return nil, fmt.Errorf("openapi: %s %s: %w", route.Method, route.Pattern, err)
		}
		operation := newOperation(route, params, usedIDs)
		for _, tag := range operation["tags"].([]string) {
			tags[tag] = true
		}
		if paths[template] == nil {
			paths[template] = map[string]any{}
		}
		if _, dup := paths[template][method]; dup {
			return nil, fmt.Errorf("openapi: %s %s is registered twice", route.Method, template)
		}
		paths[template][method] = operation
	}
	info := map[string]any{"title": opts.Title, "version": opts.Version}
	if opts.Description != "" {
		info["description"] = opts.Description
	}
	doc := map[string]any{"openapi": Version, "info": info, "paths": paths}
	if len(tags) > 0 {
		names := make([]string, 0, len(tags))
		for name := range tags {
			names = append(names, name)
		}
		sort.Strings(names)
		list := make([]map[string]any, len(names))
		for i, name := range names {
			list[i] = map[string]any{"name": name}
		}
		doc["tags"] = list
	}
	if len(opts.Servers) > 0 {
		servers := make([]map[string]any, len(opts.Servers))
		for i, url := range opts.Servers {
			servers[i] = map[string]any{"url": url}
		}
		doc["servers"] = servers
	}
	return doc, nil
}

func skip(route tango.DescribedRoute, opts Options) bool {
	if route.Doc != nil && route.Doc.Hidden {
		return true
	}
	for _, prefix := range opts.ExcludePrefixes {
		if strings.HasPrefix(route.Pattern, prefix) {
			return true
		}
	}
	return false
}

func newOperation(route tango.DescribedRoute, params []pathParam, usedIDs map[string]bool) map[string]any {
	op := map[string]any{
		"operationId": operationID(route, usedIDs),
		"responses":   map[string]any{"default": map[string]any{"description": "Response"}},
	}
	tags := []string{}
	if route.Doc != nil {
		doc := route.Doc
		if doc.Summary != "" {
			op["summary"] = doc.Summary
		}
		if doc.Description != "" {
			op["description"] = doc.Description
		}
		if doc.Deprecated {
			op["deprecated"] = true
		}
		tags = append(tags, doc.Tags...)
	}
	if len(tags) == 0 && route.Prefix != "" {
		tags = append(tags, route.Prefix)
	}
	op["tags"] = tags
	if len(params) > 0 {
		list := make([]map[string]any, len(params))
		for i, p := range params {
			param := map[string]any{"name": p.name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
			if p.wildcard {
				param["description"] = "The rest of the path."
			}
			list[i] = param
		}
		op["parameters"] = list
	}
	return op
}

// operationID is the route's name when it has one, otherwise the method and
// path made into an identifier. Duplicates get a numeric suffix.
func operationID(route tango.DescribedRoute, used map[string]bool) string {
	id := route.Name
	if id == "" {
		id = strings.ToLower(route.Method) + "_" + identifier(route.Pattern)
	}
	id = identifier(id)
	unique := id
	for n := 2; used[unique]; n++ {
		unique = fmt.Sprintf("%s_%d", id, n)
	}
	used[unique] = true
	return unique
}

func identifier(s string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

package shellcore

import (
	"errors"
	"strings"
	"testing"
)

func TestEveryVerbRefusesAnUnknownModelWithADidYouMean(t *testing.T) {
	v := newVerbs(t, false)
	calls := map[string]func() error{
		"Describe": func() error { _, err := v.Describe("blog.Nope"); return err },
		"Get":      func() error { _, err := v.Get("blog.Nope", 1); return err },
		"List":     func() error { _, err := v.List("blog.Nope"); return err },
		"Count":    func() error { _, err := v.Count("blog.Nope"); return err },
		"Create":   func() error { _, err := v.Create("blog.Nope", map[string]any{}); return err },
		"Update":   func() error { _, err := v.Update("blog.Nope", 1, map[string]any{}); return err },
		"Delete":   func() error { return v.Delete("blog.Nope", 1) },
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "Nope") {
			t.Errorf("%s of an unknown model = %v, want an error naming it", name, err)
		}
	}
	if err := v.Delete("post", 1); err != nil && !strings.Contains(err.Error(), "blog.Post") {
		t.Errorf("a case-different name did not suggest blog.Post: %v", err)
	}
}

func TestAReadOnlySessionRefusesEveryWriteVerb(t *testing.T) {
	v := newVerbs(t, true)
	for name, err := range map[string]error{
		"Create": func() error { _, err := v.Create("blog.Post", map[string]any{"Title": "x"}); return err }(),
		"Update": func() error { _, err := v.Update("blog.Post", 1, map[string]any{"Title": "x"}); return err }(),
		"Delete": v.Delete("blog.Post", 1),
	} {
		if !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s = %v, want ErrReadOnly", name, err)
		}
	}
	if _, err := v.Count("blog.Post"); err != nil {
		t.Fatalf("a read-only Count failed: %v", err)
	}
}

func TestQueryMapsAreValidatedBeforeAnythingRuns(t *testing.T) {
	v := newVerbs(t, false)
	tests := []struct {
		name  string
		query []map[string]any
		want  string
	}{
		{"two maps", []map[string]any{{}, {}}, "one query map"},
		{"where that is not a map", []map[string]any{{"where": "Title = x"}}, `"where" must be a map`},
		{"where on an unknown field", []map[string]any{{"where": map[string]any{"Nope": 1}}}, "Nope"},
		{"where with the wrong type", []map[string]any{{"where": map[string]any{"Views": "many"}}}, "Views"},
		{"order that is not a list", []map[string]any{{"order": 5}}, "order"},
	}
	for _, tt := range tests {
		if _, err := v.Count("blog.Post", tt.query...); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Count with %s = %v, want an error containing %q", tt.name, err, tt.want)
		}
		if _, err := v.List("blog.Post", tt.query...); err == nil {
			t.Errorf("List with %s succeeded", tt.name)
		}
	}
	if n, err := v.Count("blog.Post", nil); err != nil || n != 0 {
		t.Fatalf("Count with a nil map = %d, %v", n, err)
	}
}

func TestWritesRefuseValuesThatDoNotFitTheirField(t *testing.T) {
	v := newVerbs(t, false)
	if _, err := v.Create("blog.Post", map[string]any{"Title": "ok"}); err != nil {
		t.Fatal(err)
	}
	bad := []struct {
		name  string
		field string
		value any
	}{
		{"a string for a number", "Views", "12"},
		{"a number for a string", "Title", 12},
		{"nil for a non-nilable field", "Views", nil},
		{"a fractional number for an integer", "Views", 1.5},
		{"an unknown field", "Nope", 1},
		{"a number for a bool", "Published", 1},
	}
	for _, tt := range bad {
		if _, err := v.Create("blog.Post", map[string]any{tt.field: tt.value}); err == nil {
			t.Errorf("Create with %s succeeded", tt.name)
		}
		if _, err := v.Update("blog.Post", 1, map[string]any{tt.field: tt.value}); err == nil {
			t.Errorf("Update with %s succeeded", tt.name)
		}
	}
	if _, err := v.Update("blog.Post", 1, map[string]any{"Views": int8(7), "Score": 2}); err != nil {
		t.Fatalf("lossless number conversions were refused: %v", err)
	}
	if got, _ := v.Get("blog.Post", 1); got["Views"] != 7 || got["Score"] != 2.0 {
		t.Fatalf("row after conversions = %v", got)
	}
}

func TestPrimaryKeysAndMissingRowsAreReportedNotIgnored(t *testing.T) {
	v := newVerbs(t, false)
	if _, err := v.Update("blog.Post", "one", map[string]any{"Title": "x"}); err == nil || !strings.Contains(err.Error(), "primary key") {
		t.Errorf("Update with a string key = %v, want a primary-key error", err)
	}
	if err := v.Delete("blog.Post", "one"); err == nil || !strings.Contains(err.Error(), "primary key") {
		t.Errorf("Delete with a string key = %v, want a primary-key error", err)
	}
	if _, err := v.Update("blog.Post", 999, map[string]any{"Title": "x"}); err == nil || !strings.Contains(err.Error(), "update blog.Post") {
		t.Errorf("Update of a missing row = %v", err)
	}
	if _, err := v.Get("blog.Post", 999); err == nil {
		t.Error("Get of a missing row succeeded")
	}
}

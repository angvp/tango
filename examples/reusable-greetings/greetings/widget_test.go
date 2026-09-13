package greetings

import (
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

// fakeFieldValues is a minimal admin.FieldValues for testing Parse without
// a real HTTP form.
type fakeFieldValues map[string]string

func (f fakeFieldValues) Get(key string) string { return f[key] }
func (f fakeFieldValues) Has(key string) bool {
	_, ok := f[key]
	return ok
}

func TestTrimmedNameWidgetRendersInputAndOwnAsset(t *testing.T) {
	html := string(NameWidget().Render(admin.FieldContext{Name: "Name", Label: "Name", Value: "hello"}))

	if !strings.Contains(html, `name="Name"`) {
		t.Fatalf("rendered HTML does not contain the field's input:\n%s", html)
	}
	if !strings.Contains(html, `value="hello"`) {
		t.Fatalf("rendered HTML does not carry the current value:\n%s", html)
	}
	if !strings.Contains(html, `src="/greetings/static/widget.js"`) {
		t.Fatalf("rendered HTML does not reference the app's own served asset:\n%s", html)
	}
}

func TestTrimmedNameWidgetParseTrimsWhitespace(t *testing.T) {
	var g Greeting
	dest := reflect.ValueOf(&g).Elem().FieldByName("Name")

	err := NameWidget().Parse(admin.FieldContext{Name: "Name"}, fakeFieldValues{"Name": "  padded  "}, dest)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if g.Name != "padded" {
		t.Fatalf("Name = %q, want %q", g.Name, "padded")
	}
}

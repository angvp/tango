package admin

import (
	"regexp"
	"testing"
)

func TestJSONWidgetRenderHasNoInlineScriptOrHandler(t *testing.T) {
	out := string(jsonWidget{}.Render(FieldContext{Name: "Body", Label: "Body", Value: "{}"}))
	if regexp.MustCompile(`<script(\s[^>]*)?>`).FindString(out) != `<script src="`+widgetsAsset.URL+`" defer>` {
		t.Errorf("the only script tag should be the shared src tag:\n%s", out)
	}
	if regexp.MustCompile(`\son[a-z]+=`).MatchString(out) {
		t.Errorf("inline event handler attribute in:\n%s", out)
	}
}

func TestCharacterPositionCountsRunesNotBytes(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		offset int64
		want   int
	}{
		{"ascii", `{"a": 1,}`, 9, 9},
		{"multibyte before the error", `{"é": 1,}`, 10, 9},
		{"zero offset", `x`, 0, 1},
		{"offset past the end is clamped", `ab`, 99, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := characterPosition(tt.raw, tt.offset); got != tt.want {
				t.Errorf("characterPosition(%q, %d) = %d, want %d", tt.raw, tt.offset, got, tt.want)
			}
		})
	}
}

func TestWidgetsAssetURLIsHashedFromTheEmbeddedBytes(t *testing.T) {
	if !regexp.MustCompile(`^/admin/_assets/widgets\.[0-9a-f]{16}\.js$`).MatchString(widgetsAsset.URL) {
		t.Fatalf("URL = %q", widgetsAsset.URL)
	}
	other := newStaticAsset("assets/widgets.js", "text/plain")
	if other.URL != widgetsAsset.URL {
		t.Errorf("the same bytes gave different URLs: %q, %q", other.URL, widgetsAsset.URL)
	}
}

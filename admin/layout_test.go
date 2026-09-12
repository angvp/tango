package admin

import (
	"bytes"
	"strings"
	"testing"
)

func TestBaseLayoutRendersTailwindScript(t *testing.T) {
	var buf bytes.Buffer
	if err := freshLayout().ExecuteTemplate(&buf, "layout", nil); err != nil {
		t.Fatalf("ExecuteTemplate returned error: %v", err)
	}

	if !strings.Contains(buf.String(), tailwindScriptTag) {
		t.Fatalf("layout output does not contain the Tailwind script tag: %s", buf.String())
	}
}

func TestBaseLayoutRendersContentBlock(t *testing.T) {
	tmpl := cloneWithContent(`{{define "content"}}<p>hello from content</p>{{end}}`)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", nil); err != nil {
		t.Fatalf("ExecuteTemplate returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "hello from content") {
		t.Fatalf("layout output does not contain the page content: %s", buf.String())
	}
}

func TestBaseLayoutProducesValidHTMLStructure(t *testing.T) {
	var buf bytes.Buffer
	if err := freshLayout().ExecuteTemplate(&buf, "layout", nil); err != nil {
		t.Fatalf("ExecuteTemplate returned error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"<html", "<head>", "<body>", "</html>"} {
		if !strings.Contains(output, want) {
			t.Fatalf("layout output missing %q: %s", want, output)
		}
	}

	if strings.Count(output, "<html") != 1 || strings.Count(output, "</html>") != 1 {
		t.Fatalf("layout output does not have exactly one <html>/</html> pair: %s", output)
	}
}

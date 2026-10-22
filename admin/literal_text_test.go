package admin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	templateActions = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	embeddedCode    = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	markupTags      = regexp.MustCompile(`(?s)<[^>]*>?`)
	htmlEntities    = regexp.MustCompile(`&#?\w+;`)
	visibleWord     = regexp.MustCompile(`[A-Za-z]{2,}`)
	partialTag      = regexp.MustCompile(`(?s)^[^<]*>`)
	statusClass     = regexp.MustCompile(`\dxx`)
	textAttribute   = regexp.MustCompile(`(?i)\b(aria-label|title|placeholder|alt)="[^"{]*[A-Za-z]`)
)

// TestTemplateTextGoesThroughT keeps admin's HTML templates translatable:
// words that reach the page must come from a {{.T ...}} (or other template
// action), never sit in the template as literal text, and text-bearing
// attributes must be actions too.
func TestTemplateTextGoesThroughT(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || !strings.HasPrefix(lit.Value, "`") {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil || !strings.Contains(text, "<") {
				return true
			}
			where := fset.Position(lit.Pos()).String()
			text = templateActions.ReplaceAllString(text, "")
			if match := textAttribute.FindString(text); match != "" {
				t.Errorf("%s: literal text in attribute %q; render it with {{.T ...}}", where, match)
			}
			text = embeddedCode.ReplaceAllString(text, "")
			if !strings.HasPrefix(strings.TrimSpace(text), "<") {
				text = partialTag.ReplaceAllString(text, "")
			}
			text = markupTags.ReplaceAllString(text, "")
			text = statusClass.ReplaceAllString(text, "")
			text = htmlEntities.ReplaceAllString(text, "")
			if word := visibleWord.FindString(text); word != "" {
				t.Errorf("%s: literal text %q in template; render it with {{.T ...}}", where, strings.TrimSpace(text))
			}
			return true
		})
	}
}

// TestNoLiteralFieldErrorMessages keeps widget validation text translatable:
// admin's own FieldErrors carry a key through localizedText.
func TestNoLiteralFieldErrorMessages(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			comp, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := comp.Type.(*ast.Ident); !ok || id.Name != "FieldError" {
				return true
			}
			hasText := false
			for _, elt := range comp.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "text" {
						hasText = true
					}
				}
			}
			if !hasText && len(comp.Elts) > 0 {
				t.Errorf("%s: FieldError without translatable text", fset.Position(comp.Pos()))
			}
			return true
		})
	}
}

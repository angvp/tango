package admin

import "testing"

func TestSafeHrefAcceptsOnlyRelativeOrHTTPURLs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"root-relative path", "/documents/7/download/", true},
		{"path-relative", "documents/7/download/", true},
		{"relative with query", "/documents/?page=2", true},
		{"fragment only", "#files", true},
		{"absolute https", "https://files.example.com/a", true},
		{"absolute http", "http://files.example.com/a", true},
		{"upper-case scheme", "HTTPS://files.example.com/a", true},
		{"empty", "", false},
		{"scheme-relative host", "//evil.example/x", false},
		{"backslash scheme-relative", `/\evil.example/x`, false},
		{"backslash path", `\\evil.example\x`, false},
		{"javascript scheme", "javascript:alert(1)", false},
		{"upper-case javascript", "JaVaScRiPt:alert(1)", false},
		{"javascript with leading space", " javascript:alert(1)", false},
		{"javascript with embedded tab", "java\tscript:alert(1)", false},
		{"javascript with newline", "java\nscript:alert(1)", false},
		{"javascript with NUL", "javascript\x00:alert(1)", false},
		{"data scheme", "data:text/html,<script>alert(1)</script>", false},
		{"mailto", "mailto:ops@example.com", false},
		{"ftp absolute", "ftp://files.example.com/a", false},
		{"http with no host", "https:///a", false},
		{"userinfo", "https://user@files.example.com/a", false},
		{"trailing space", "/documents/ ", false},
		{"delete byte", "/a\x7fb", false},
		{"invalid percent escape", "/a%zz", false},
		{"bare scheme-like text", "javascript&colon;alert(1)", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeHref(tt.raw)
			if ok != tt.want {
				t.Fatalf("safeHref(%q) ok = %v, want %v", tt.raw, ok, tt.want)
			}
			if ok && got != tt.raw {
				t.Fatalf("safeHref(%q) = %q, want the input unchanged", tt.raw, got)
			}
		})
	}
}

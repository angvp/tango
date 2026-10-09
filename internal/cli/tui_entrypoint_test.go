package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTUIWithEntrypointsThatCannotAnswerStatus runs the real go toolchain
// against small projects whose main.go predates -tango-status, instead of a
// runner that fakes their output.
func TestTUIWithEntrypointsThatCannotAnswerStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go run on throwaway projects")
	}
	tests := []struct {
		name   string
		main   string
		rawHas string
	}{
		{"it prints something else and exits", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n", "decode status"},
		{"it parses flags and rejects the unknown one", "package main\n\nimport \"flag\"\n\nfunc main() { flag.Parse() }\n", "-tango-status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range map[string]string{"go.mod": "module oldproject\n\ngo 1.27\n", "main.go": tt.main} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr strings.Builder
			code := tui(context.Background(), ExecRunner{}, dir, &stdout, &stderr, func() bool { return false }, nil)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1; stderr:\n%s", code, stderr.String())
			}
			out := stderr.String()
			explanation := strings.Index(out, "tanGO could not load project status")
			raw := strings.Index(out, tt.rawHas)
			if explanation < 0 || raw < 0 || explanation > raw {
				t.Fatalf("stderr must explain first and keep the raw error (%q):\n%s", tt.rawHas, out)
			}
		})
	}
}

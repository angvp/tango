package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAppGeneratesStubWithoutTouchingExistingFiles(t *testing.T) {
	dir := t.TempDir()

	mainGoPath := filepath.Join(dir, "main.go")
	mainGoContent := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(mainGoPath, []byte(mainGoContent), 0o644); err != nil {
		t.Fatalf("seed main.go: %v", err)
	}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"newapp", "posts"}, dir, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	appFile := filepath.Join(dir, "apps", "posts", "app.go")
	content, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	source := string(content)

	for _, want := range []string{
		"package posts",
		`"github.com/angvp/tango"`,
		"func (App) Name() string",
		`return "posts"`,
		"func (App) Register(registry *tango.Registry) error",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("app.go does not contain %q:\n%s", want, source)
		}
	}

	after, err := os.ReadFile(mainGoPath)
	if err != nil {
		t.Fatalf("re-read main.go: %v", err)
	}
	if string(after) != mainGoContent {
		t.Fatalf("main.go was modified, want unchanged:\ngot:  %q\nwant: %q", after, mainGoContent)
	}
}

func TestNewAppFailsWhenAppAlreadyExists(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"newapp", "posts"}, dir, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("first newapp exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	appFile := filepath.Join(dir, "apps", "posts", "app.go")
	before, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"newapp", "posts"}, dir, &stdout, &stderr, nil)
	if code == 0 {
		t.Fatalf("second newapp exit code = %d, want non-zero", code)
	}

	after, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("re-read app.go: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("app.go was overwritten by second newapp run")
	}
}

func TestNewAppRequiresName(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	code := Run(context.Background(), []string{"newapp"}, dir, os.Stdout, &stderr, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

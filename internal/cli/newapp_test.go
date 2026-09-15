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

// TestNewAppReportsFailures table-drives newapp's failure paths: an
// explicit empty name, a Stat that can't determine whether the target
// exists, a read-only apps directory, and a name that isn't a valid Go
// identifier.
func TestNewAppReportsFailures(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		skip       func(t *testing.T) bool
		setup      func(t *testing.T, dir string)
		wantCode   int
		wantStderr string
		postCheck  func(t *testing.T, dir string)
	}{
		{
			// Covers newApp's own name=="" guard, distinct from Run's
			// "newapp" dispatch requiring at least one argument
			// (TestNewAppRequiresName): an explicit empty string still
			// reaches newApp.
			name:       "rejects explicit empty name",
			args:       []string{"newapp", ""},
			wantCode:   2,
			wantStderr: "an app name is required",
		},
		{
			// "apps" as a plain file makes Stat(apps/posts/app.go) fail with
			// ENOTDIR, which os.IsNotExist reports as false — distinct from
			// the ordinary not-exist path.
			name: "stat cannot determine existence",
			args: []string{"newapp", "posts"},
			setup: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "apps"), []byte("not a directory"), 0o644); err != nil {
					t.Fatalf("seed blocking file: %v", err)
				}
			},
			wantCode: 1,
		},
		{
			name: "apps directory is not writable",
			args: []string{"newapp", "posts"},
			skip: func(t *testing.T) bool { return os.Geteuid() == 0 },
			setup: func(t *testing.T, dir string) {
				appsDir := filepath.Join(dir, "apps")
				if err := os.MkdirAll(appsDir, 0o555); err != nil {
					t.Fatalf("mkdir read-only apps dir: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(appsDir, 0o755) })
			},
			wantCode: 1,
		},
		{
			// Documents current behavior: newapp does not validate that name
			// is a legal Go package identifier, so a name like "1bad"
			// produces an invalid app.go that fails to gofmt — surfaced as a
			// clear error rather than a silently broken file.
			name:     "name is not a valid Go identifier",
			args:     []string{"newapp", "1bad"},
			wantCode: 1,
			postCheck: func(t *testing.T, dir string) {
				if _, err := os.Stat(filepath.Join(dir, "apps", "1bad", "app.go")); !os.IsNotExist(err) {
					t.Fatalf("app.go stat = %v, want not exist (invalid source must not be left behind)", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil && tt.skip(t) {
				t.Skip("root ignores directory permission bits")
			}
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}

			var stderr strings.Builder
			code := Run(context.Background(), tt.args, dir, os.Stdout, &stderr, nil)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d, stderr: %s", code, tt.wantCode, stderr.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if tt.postCheck != nil {
				tt.postCheck(t, dir)
			}
		})
	}
}

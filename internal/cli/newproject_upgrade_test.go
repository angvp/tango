package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The documented upgrade of a project made before the shell existed is a
// diff per scaffold (testdata/legacy_scaffold/upgrade_*.diff): the app list
// and configuration move into project/project.go, main.go calls it, and
// shell/main.go is added. These tests prove that applying it to the real
// pre-shell scaffold gives a project that builds, runs and opens a shell,
// and that the diff still produces exactly what `tango newproject` writes.
func TestUpgradingAPreShellProjectFollowsTheDocumentedDiff(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs projects")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git applies the documented diff")
	}
	for _, variant := range []struct {
		name       string
		withAdmin  bool
		wantModels string
	}{
		{"admin", true, `["admin.AdminSession" "admin.AdminUser"]`},
		{"noadmin", false, `[]`},
	} {
		t.Run(variant.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "legacyapp")
			legacy := filepath.Join("testdata", "legacy_scaffold", variant.name)
			for from, to := range map[string]string{
				"main.go.txt":       "main.go",
				"migrations.go.txt": filepath.Join("migrations", "migrations.go"),
				"gitignore.txt":     ".gitignore",
			} {
				content, err := os.ReadFile(filepath.Join(legacy, from))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, to)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, to), content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			runIn := func(name string, args ...string) {
				t.Helper()
				cmd := exec.Command(name, args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s %v: %v\n%s", name, args, err, out)
				}
			}
			runIn("go", "mod", "init", "legacyapp")
			runIn("go", "mod", "edit", "-replace", "github.com/angvp/tango="+repo)
			runIn("go", "mod", "tidy")
			t.Setenv("TANGO_DB_DSN", "sqlite://"+filepath.Join(dir, "app.db"))

			// Before: the project is a working server with no shell.
			runIn("go", "build", "-o", filepath.Join(t.TempDir(), "before"), ".")
			var stderr bytes.Buffer
			if code := Run(context.Background(), []string{"shell"}, dir, os.Stdout, &stderr, ExecRunner{}); code != 1 ||
				!strings.Contains(stderr.String(), "shell/main.go") || !strings.Contains(stderr.String(), "adding-the-shell-to-an-existing-project") {
				t.Fatalf("shell before the upgrade: code=%d stderr=%q, want the missing file and the guide's upgrade section", code, stderr.String())
			}

			// The documented step.
			runIn("git", "apply", "--unsafe-paths", filepath.Join(mustAbs(t, legacy), "..", "upgrade_"+variant.name+".diff"))

			// After: byte for byte what newproject writes today.
			dialect := projectDialects["sqlite"]
			for path, want := range map[string]string{
				"main.go":            renderNewProjectMain("legacyapp", dialect, variant.withAdmin),
				"project/project.go": renderProjectGo(variant.withAdmin),
				"shell/main.go":      renderShellMain("legacyapp", dialect),
			} {
				got, err := os.ReadFile(filepath.Join(dir, path))
				if err != nil {
					t.Fatal(err)
				}
				formatted := formatForCompare(t, want)
				if string(got) != formatted {
					t.Fatalf("%s after the upgrade differs from what tango newproject writes now.\nRegenerate testdata/legacy_scaffold/upgrade_%s.diff.\ngot:\n%s\nwant:\n%s", path, variant.name, got, formatted)
				}
			}

			runIn("go", "mod", "tidy")
			runIn("go", "vet", "./...")
			runIn("go", "run", ".", "-check")
			runIn("go", "run", ".", "-migrate")
			var out, errOut bytes.Buffer
			if code := Run(context.Background(), []string{"shell", "-c", "Models()"}, dir, &out, &errOut, ExecRunner{}); code != 0 || out.String() != variant.wantModels+"\n" {
				t.Fatalf("shell after the upgrade: code=%d stdout=%q stderr=%q, want %s", code, out.String(), errOut.String(), variant.wantModels)
			}
		})
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// formatForCompare gofmts generated source the way newproject writes it.
func formatForCompare(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.go")
	if err := writeFormattedFile(path, source); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

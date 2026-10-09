package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildScaffold generates a project with the given newproject arguments,
// builds it against this checkout and returns the binary and project dir.
func buildScaffold(t *testing.T, name string, flags ...string) (binary, project string) {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var stderr strings.Builder
	args := append([]string{"newproject"}, flags...)
	args = append(args, name)
	if code := Run(context.Background(), args, dir, io.Discard, &stderr, localTangoRunner{repo: repo}); code != 0 {
		t.Fatalf("newproject exit code = %d: %s", code, stderr.String())
	}
	project = filepath.Join(dir, name)
	binary = filepath.Join(dir, name+"-bin")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = project
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binary, project
}

// startupOutput runs the binary with TANGO_ADDR=addr until it has printed
// "listening on" and returns everything printed up to the stop.
func startupOutput(t *testing.T, binary, project, addr string) string {
	t.Helper()
	server := exec.Command(binary)
	var output lockedBuffer
	server.Dir, server.Stdout, server.Stderr = project, &output, &output
	server.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TANGO_ADDR=" + addr,
		"TANGO_DB_DSN=sqlite://" + filepath.Join(project, "app.db")}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
	for deadline := time.Now().Add(20 * time.Second); !strings.Contains(output.String(), "listening on"); {
		if time.Now().After(deadline) {
			t.Fatalf("never printed listening on:\n%s", output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // let the line after "listening on" flush
	return output.String()
}

func TestScaffoldPrintsTheAdminURLAfterListeningOn(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs generated projects")
	}
	binary, project := buildScaffold(t, "shop")
	tests := []struct {
		addr string
		want string // empty: no admin line
	}{
		{":8000", "admin: http://localhost:8000/admin/"},
		{"127.0.0.1:9000", "admin: http://127.0.0.1:9000/admin/"},
		{"0.0.0.0:8000", "admin: http://localhost:8000/admin/"},
		{"[::]:8000", "admin: http://localhost:8000/admin/"},
		{"not-an-address", ""},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			out := startupOutput(t, binary, project, tt.addr)
			if tt.want == "" {
				if strings.Contains(out, "admin:") {
					t.Fatalf("printed an admin line for an unparsable address:\n%s", out)
				}
				return
			}
			listening := strings.Index(out, "listening on")
			admin := strings.Index(out, tt.want)
			if admin < 0 || admin < listening {
				t.Fatalf("want %q after listening on, got:\n%s", tt.want, out)
			}
		})
	}
}

func TestScaffoldWithoutAdminPrintsNoAdminURL(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated project")
	}
	binary, project := buildScaffold(t, "plain", "--no-admin")
	if out := startupOutput(t, binary, project, ":8000"); strings.Contains(out, "admin:") {
		t.Fatalf("a --no-admin project printed an admin line:\n%s", out)
	}
}

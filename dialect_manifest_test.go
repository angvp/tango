package tango_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// packagesFor runs scripts/dialect-packages.sh the way CI does and returns
// its output, one package pattern per line.
func packagesFor(t *testing.T, dsn, manifest string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "scripts/dialect-packages.sh")
	cmd.Env = append(os.Environ(), "TANGO_TEST_DSN="+dsn, "DIALECT_MANIFEST="+manifest)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	// A failure explains itself on stderr; a success is judged on stdout alone.
	return strings.TrimSpace(stdout.String() + stderr.String()), err
}

func TestSQLiteRunsEveryPackage(t *testing.T) {
	for _, dsn := range []string{"", "sqlite://:memory:"} {
		got, err := packagesFor(t, dsn, "scripts/dialect-manifest.txt")
		if err != nil || got != "./..." {
			t.Errorf("dsn %q: packages = %q, err = %v; want ./...", dsn, got, err)
		}
	}
}

func TestPostgresAndMySQLRunOnlyTheDialectSensitivePackages(t *testing.T) {
	for _, dsn := range []string{
		"postgres://u:p@localhost:5432/t?sslmode=disable",
		"mysql://root:p@127.0.0.1:3306/mysql",
	} {
		got, err := packagesFor(t, dsn, "scripts/dialect-manifest.txt")
		if err != nil {
			t.Fatalf("dsn %q: %v\n%s", dsn, err, got)
		}
		lines := strings.Fields(got)
		has := map[string]bool{}
		for _, l := range lines {
			has[l] = true
		}
		for _, want := range []string{"./db", "./migration", "./testdb", "./admin"} {
			if !has[want] {
				t.Errorf("dsn %q: %s missing from %v", dsn, want, lines)
			}
		}
		for _, notWant := range []string{"./", ".", "./...", "./auth/jwt", "./mail"} {
			if has[notWant] {
				t.Errorf("dsn %q: %s must not run on this dialect", dsn, notWant)
			}
		}
	}
}

func TestAnUnclassifiedPackageFailsTheRunner(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "manifest.txt")
	if err := os.WriteFile(manifest, []byte("sensitive ./db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{"", "postgres://u:p@h/t"} {
		out, err := packagesFor(t, dsn, manifest)
		if err == nil || !strings.Contains(out, "not classified") || !strings.Contains(out, "./migration") {
			t.Errorf("dsn %q: want a failure naming an unclassified package, got err=%v out=%q", dsn, err, out)
		}
	}
}

func TestAStaleManifestEntryFailsTheRunner(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "manifest.txt")
	all, err := os.ReadFile("scripts/dialect-manifest.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(all, []byte("sensitive ./gone\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := packagesFor(t, "", manifest)
	if err == nil || !strings.Contains(out, "./gone") {
		t.Errorf("want a failure naming ./gone, got err=%v out=%q", err, out)
	}
}

package tango_test

// This file keeps the documentation's examples honest:
// every checked-in example under examples/ must compile as its own module,
// pass its own -check and `go test ./...`, and must depend on the real
// module path — so a doc snippet quoting these examples can never silently
// drift from what actually builds.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExamplesAreIndependentModulesThatCompile(t *testing.T) {
	entries, err := os.ReadDir("examples")
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}

	var found int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		exampleDir := filepath.Join("examples", entry.Name())
		goModPath := filepath.Join(exampleDir, "go.mod")

		content, err := os.ReadFile(goModPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", goModPath, err)
		}
		found++

		if !strings.Contains(string(content), "module ") {
			t.Fatalf("%s does not declare its own module — examples must be separate Go modules", goModPath)
		}
		if !strings.Contains(string(content), "github.com/angvp/tango") {
			t.Fatalf("%s does not depend on github.com/angvp/tango — a documented example must import the real module path", goModPath)
		}

		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = exampleDir
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("go build in %s failed: %v\n%s", exampleDir, err, out.String())
		}

		checkCmd := exec.Command("go", "run", ".", "-check")
		checkCmd.Dir = exampleDir
		checkCmd.Env = append(os.Environ(),
			"TANGO_ADMIN_PASSWORD=test-password",
			// examples/board (the tutorial app) refuses to start without its
			// API token secret, which lives in an untracked .env locally.
			"BOARD_JWT_SECRET="+strings.Repeat("s", 32),
			// examples/spa-accounts signs its access tokens with this.
			"SPA_JWT_SECRET="+strings.Repeat("s", 32),
			// examples/uploads keeps uploaded files here; -check opens it. It must
			// not exist yet: storage/local refuses an existing directory that is
			// open to group or others, and t.TempDir() is.
			"UPLOADS_DIR="+filepath.Join(t.TempDir(), "uploads"), // storage/local creates it private (0700)
		)
		var checkOut strings.Builder
		checkCmd.Stdout = &checkOut
		checkCmd.Stderr = &checkOut
		if err := checkCmd.Run(); err != nil {
			t.Fatalf("go run . -check in %s failed: %v\n%s", exampleDir, err, checkOut.String())
		}
		if !strings.Contains(checkOut.String(), "check passed") {
			t.Fatalf("go run . -check in %s did not report success:\n%s", exampleDir, checkOut.String())
		}

		// Each example's own tests run with the caller's environment, so a
		// dialect-aware example sees the same TANGO_TEST_DSN as the suite.
		testCmd := exec.Command("go", "test", "./...")
		testCmd.Dir = exampleDir
		testCmd.Env = os.Environ()
		var testOut strings.Builder
		testCmd.Stdout = &testOut
		testCmd.Stderr = &testOut
		if err := testCmd.Run(); err != nil {
			t.Fatalf("go test ./... in %s failed: %v\n%s", exampleDir, err, testOut.String())
		}
	}

	if found == 0 {
		t.Fatal("no example modules found under examples/ — expected at least jsonapi and api-with-admin")
	}
}

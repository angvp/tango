package tango_test

// Every example registers migrations that its own runtime applies, so a
// registry that Validate rejects would only surface when that example runs.
// The examples are separate modules, so the check runs inside each one: an
// overlay (go test -overlay) supplies a test file at a path under the example,
// and nothing is written under examples/. Build output goes to the Go build
// cache, not the tree.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const registryTestName = "TestTangoRegistryValid"

// registryTestFile is the overlay's file name. It is never created on disk.
const registryTestFile = "zz_tango_registry_test.go"

// migrationsRegistryTest checks the aggregate registry an example's main.go
// hands to the runtime.
const migrationsRegistryTest = `package migrations

import (
	"testing"

	"github.com/angvp/tango/migration"
)

func TestTangoRegistryValid(t *testing.T) {
	if len(Migrations) == 0 {
		t.Skip("no migrations registered")
	}
	if err := migration.Validate(Migrations); err != nil {
		t.Fatalf("registry invalid: %v", err)
	}
	t.Logf("validated %d migrations", len(Migrations))
}
`

// composedRegistryTest checks a registry that package main assembles from
// several packages. Its migrations package alone does not describe what runs.
const composedRegistryTest = `package main

import (
	"testing"

	"github.com/angvp/tango/migration"
)

func TestTangoRegistryValid(t *testing.T) {
	all := allMigrations()
	if len(all) == 0 {
		t.Skip("no migrations registered")
	}
	if err := migration.Validate(all); err != nil {
		t.Fatalf("registry invalid: %v", err)
	}
	t.Logf("validated %d migrations", len(all))
}
`

// composedRegistries names the examples whose runnable registry comes from
// package main's allMigrations rather than migrations.Migrations. This is the
// only per-example knowledge in the test; the examples themselves are
// discovered by glob.
var composedRegistries = map[string]bool{
	"reusable-greetings-host": true,
}

const (
	statusValidated    = "validated"
	statusNoMigrations = "no migrations"
)

func TestExamplesRegistriesValidate(t *testing.T) {
	dirs, err := discoverExamples()
	if err != nil {
		t.Fatalf("discovering examples: %v", err)
	}
	if len(dirs) == 0 {
		t.Fatal("no example modules discovered under examples/; the check would pass vacuously")
	}

	statuses := make(map[string]string, len(dirs))
	for _, dir := range dirs {
		name := filepath.Base(dir)
		statuses[name] = checkExampleRegistry(t, dir, name)
	}

	for _, name := range sortedKeys(statuses) {
		t.Logf("example %-26s %s", name, statuses[name])
	}
}

// discoverExamples returns every examples/<name> directory that is a module
// (has go.mod) or a program (has main.go). An entry with main.go but no go.mod
// is a failure, because the examples must each be their own module.
func discoverExamples() ([]string, error) {
	modules, err := filepath.Glob(filepath.Join("examples", "*", "go.mod"))
	if err != nil {
		return nil, err
	}
	programs, err := filepath.Glob(filepath.Join("examples", "*", "main.go"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var dirs []string
	for _, file := range append(modules, programs...) {
		dir := filepath.Dir(file)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			return nil, err
		}
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// checkExampleRegistry validates one example's registry and returns the
// status it reports. A missing registry is reported as "no migrations",
// never skipped.
func checkExampleRegistry(t *testing.T, dir string, name string) string {
	t.Helper()
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	var pkg, body, overlayTarget string
	if composedRegistries[name] {
		pkg, body, overlayTarget = ".", composedRegistryTest, filepath.Join(absDir, registryTestFile)
	} else {
		migrationsDir := filepath.Join(absDir, "migrations")
		if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
			return statusNoMigrations
		} else if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pkg, body, overlayTarget = "./migrations", migrationsRegistryTest, filepath.Join(migrationsDir, registryTestFile)
	}

	scratch := t.TempDir()
	testSource := filepath.Join(scratch, registryTestFile)
	if err := os.WriteFile(testSource, []byte(body), 0o644); err != nil {
		t.Fatalf("%s: writing overlay source: %v", name, err)
	}
	overlayPath := filepath.Join(scratch, "overlay.json")
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {overlayTarget: testSource}})
	if err != nil {
		t.Fatalf("%s: encoding overlay: %v", name, err)
	}
	if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
		t.Fatalf("%s: writing overlay: %v", name, err)
	}

	cmd := exec.Command("go", "test", "-overlay", overlayPath, "-count=1", "-v",
		"-run", "^"+registryTestName+"$", pkg)
	cmd.Dir = absDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: registry does not validate (go test %s): %v\n%s", name, pkg, err, out.String())
	}

	output := out.String()
	switch {
	case strings.Contains(output, "--- SKIP: "+registryTestName+" "):
		return statusNoMigrations
	case strings.Contains(output, "--- PASS: "+registryTestName+" "):
		t.Logf("%s: %s", name, strings.TrimSpace(lineContaining(output, "validated ")))
		return statusValidated
	default:
		t.Fatalf("%s: registry test neither passed nor skipped:\n%s", name, output)
		return ""
	}
}

// lineContaining returns the first line of output containing want, or "" if
// none does. It is used only to echo the example's own validation log.
func lineContaining(output string, want string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

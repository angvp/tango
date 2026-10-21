package tango_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const testdbImport = `"github.com/angvp/tango/testdb"`

// manifestEntry is one line of scripts/dialect-manifest.txt.
type manifestEntry struct {
	class, pkg string
	reason     bool
}

func readManifest(t *testing.T, path string) []manifestEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	for _, line := range strings.Split(string(data), "\n") {
		body, comment, hasComment := strings.Cut(line, "#")
		fields := strings.Fields(body)
		if len(fields) < 2 {
			continue
		}
		entries = append(entries, manifestEntry{
			class:  fields[0],
			pkg:    fields[1],
			reason: hasComment && strings.TrimSpace(comment) != "",
		})
	}
	return entries
}

// importsTestdb reports whether any _test.go file in dir imports testdb
// directly. Reaching a database through a helper or db.Store is not a signal
// here: such a package is classified by decision, not by symbol name.
func importsTestdb(t *testing.T, dir string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imp := range parsed.Imports {
			if imp.Path.Value == testdbImport {
				return true
			}
		}
	}
	return false
}

// unjustifiedSQLiteOnly lists the sqlite-only packages that import testdb but
// give no reason in the manifest. Using testdb means the package reaches a
// database; keeping it off PostgreSQL and MySQL has to be a stated decision.
func unjustifiedSQLiteOnly(t *testing.T, root, manifest string) []string {
	t.Helper()
	var found []string
	for _, e := range readManifest(t, manifest) {
		if e.class != "sqlite-only" || e.reason {
			continue
		}
		if importsTestdb(t, filepath.Join(root, filepath.FromSlash(e.pkg))) {
			found = append(found, e.pkg)
		}
	}
	sort.Strings(found)
	return found
}

func TestSQLiteOnlyPackagesThatUseTestdbExplainWhy(t *testing.T) {
	got := unjustifiedSQLiteOnly(t, ".", "scripts/dialect-manifest.txt")
	for _, pkg := range got {
		t.Errorf("%s imports testdb in its tests but is sqlite-only without a reason: "+
			"mark it sensitive in scripts/dialect-manifest.txt, or add \"# why\" to its sqlite-only line "+
			"(or call testdb.SQLiteOnly in the tests that do not depend on the dialect)", pkg)
	}
}

func TestScannerFlagsAnUndeclaredTestdbUser(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("direct/x_test.go", fmt.Sprintf("package direct\nimport %s\nvar _ = testdb.Dialect\n", testdbImport))
	write("declared/x_test.go", fmt.Sprintf("package declared\nimport %s\nvar _ = testdb.Dialect\n", testdbImport))
	write("helperonly/x_test.go", "package helperonly\nfunc buildStore() {}\n")
	write("sensitive/x_test.go", fmt.Sprintf("package sensitive\nimport %s\nvar _ = testdb.Dialect\n", testdbImport))
	manifest := filepath.Join(root, "manifest.txt")
	write("manifest.txt", "sqlite-only ./direct\nsqlite-only ./declared  # no SQL asserted\nsqlite-only ./helperonly\nsensitive ./sensitive\n")

	got := unjustifiedSQLiteOnly(t, root, manifest)
	if len(got) != 1 || got[0] != "./direct" {
		t.Errorf("flagged %v, want only ./direct (a helper-only package is classified by decision, not by name)", got)
	}
}

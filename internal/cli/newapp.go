package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// newApp scaffolds a stub app package (apps/<name>/app.go) implementing the
// App interface, without touching any existing file. Wiring the new app
// into InstalledApps remains a manual, documented step (see
// docs/guides/project-structure.md), so newApp prints the exact line to add.
func newApp(dir string, name string, stdout io.Writer, stderr io.Writer) int {
	if name == "" {
		fmt.Fprintln(stderr, "tango newapp: an app name is required")
		return 2
	}

	appDir := filepath.Join(dir, "apps", name)
	appFile := filepath.Join(appDir, "app.go")

	if _, err := os.Stat(appFile); err == nil {
		fmt.Fprintf(stderr, "tango newapp: %s already exists\n", appFile)
		return 1
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "tango newapp: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(appDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "tango newapp: %v\n", err)
		return 1
	}

	source := fmt.Sprintf(newAppGoTemplate, appPackageName(name), name, name)
	if err := writeFormattedFile(appFile, source); err != nil {
		fmt.Fprintf(stderr, "tango newapp: %v\n", err)
		return 1
	}

	rel, err := filepath.Rel(dir, appFile)
	if err != nil {
		rel = appFile
	}
	fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(rel))
	fmt.Fprintln(stdout, installHint(dir, name))
	return 0
}

// installHint tells the person how to install the new app: the import path
// (when dir's go.mod names its module) and the InstalledApps entry.
func installHint(dir, name string) string {
	importPart := "import the apps/" + name + " package"
	if module := modulePath(dir); module != "" {
		importPart = fmt.Sprintf("import %q", module+"/apps/"+name)
	}
	// A project scaffolded with a project package lists its apps there.
	file := "main.go"
	if _, err := os.Stat(filepath.Join(dir, "project", "project.go")); err == nil {
		file = "project/project.go"
	}
	return fmt.Sprintf("Install it in %s: %s and add %s.App{} to config.InstalledApps.", file, importPart, appPackageName(name))
}

// modulePath returns the module path dir's go.mod declares, or "" when
// there is no go.mod or it can't be read.
func modulePath(dir string) string {
	content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		line, _, _ = strings.Cut(line, "//")
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

// appPackageName derives a Go package name from an app name; app names are
// expected to already be valid identifiers (e.g. "posts", "users").
func appPackageName(name string) string {
	return name
}

const newAppGoTemplate = `package %s

import "github.com/angvp/tango"

// App implements tango.App for the %q app.
type App struct{}

func (App) Name() string {
	return %q
}

func (App) Register(registry *tango.Registry) error {
	return nil
}
`

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// newApp scaffolds a stub app package (apps/<name>/app.go) implementing the
// App interface, without touching any existing file. Wiring the new app
// into InstalledApps remains a manual, documented step (see Milestone 8.2).
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
	return 0
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

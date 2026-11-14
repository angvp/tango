package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const tangoModule = "github.com/angvp/tango"

// exactSchemaRelease is the first tango version that reads the exact-schema
// tag options.
var exactSchemaRelease = [3]int{0, 10, 0}

// warnOldLibrary prints a warning to stderr when the app's go.mod requires
// tango below v0.10.0: that library ignores the exact-schema tag options, so
// the generated migration would silently lack them. The check is advisory and
// silent whenever the answer is unclear: no go.mod, no requirement, an
// unparsable version.
func warnOldLibrary(dir string, stderr io.Writer) {
	version, ok := requiredTangoVersion(dir)
	if !ok || version == "" {
		return
	}
	parsed, ok := parseSemver(version)
	if !ok || !semverLess(parsed, exactSchemaRelease) {
		return
	}
	fmt.Fprintf(stderr, "tango makemigrations: warning: this app requires %s %s, which ignores the exact-schema tag options (int4, naive, notnull, default=, unique=, index=) added in v0.10.0; upgrade the library or the generated migration will silently lack them\n", tangoModule, version)
}

// requiredTangoVersion finds go.mod in dir or the nearest parent, as go run .
// does, and returns the version of the tango requirement.
func requiredTangoVersion(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for current := abs; ; current = filepath.Dir(current) {
		data, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil {
			return tangoRequirement(string(data))
		}
		if !os.IsNotExist(err) || filepath.Dir(current) == current {
			return "", false
		}
	}
}

// tangoRequirement reads the tango version from a go.mod's require directives,
// single-line or in a block. Other directives (replace, exclude, retract) are
// skipped.
func tangoRequirement(gomod string) (string, bool) {
	if replacesTango(gomod) {
		// A replace points at a checkout or a fork whose version go.mod
		// does not say; stay silent rather than guess.
		return "", true
	}
	block := ""
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if block != "" {
			if fields[0] == ")" {
				block = ""
				continue
			}
			if block == "require" && fields[0] == tangoModule && len(fields) >= 2 {
				return fields[1], true
			}
			continue
		}
		switch {
		case len(fields) == 2 && fields[1] == "(":
			block = fields[0]
		case fields[0] == "require" && len(fields) >= 3 && fields[1] == tangoModule:
			return fields[2], true
		}
	}
	return "", false
}

func parseSemver(version string) ([3]int, bool) {
	core := strings.TrimPrefix(version, "v")
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	var out [3]int
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func semverLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// replacesTango reports whether go.mod has a replace directive for tango.
func replacesTango(gomod string) bool {
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case inBlock && fields[0] == ")":
			inBlock = false
		case inBlock && fields[0] == tangoModule:
			return true
		case len(fields) == 2 && fields[0] == "replace" && fields[1] == "(":
			inBlock = true
		case len(fields) >= 2 && fields[0] == "replace" && fields[1] == tangoModule:
			return true
		}
	}
	return false
}

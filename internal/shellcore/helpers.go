package shellcore

import (
	"fmt"
	"go/token"
	"sort"
)

// HelperQualifier is how a session reaches the project's own helpers:
// project.Name(...).
const HelperQualifier = "project"

// ExportHelpers makes a project's helpers callable as project.Name. Every
// name must be an exported Go identifier, and a nil value is refused. A
// helper that is a function is called like any function; anything else is a
// constant of the session. Only plain values (primitives, maps, slices and
// errors) are supported across the boundary: a value of a project type
// arrives, but using its fields or methods from the session is not
// something the shell promises.
func ExportHelpers(session *Session, helpers map[string]any) error {
	names := make([]string, 0, len(helpers))
	for name := range helpers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !token.IsIdentifier(name) || !token.IsExported(name) {
			return fmt.Errorf("invalid helper name %q: helpers are called as project.Name, so a name must be an exported Go identifier such as Reindex", name)
		}
		if helpers[name] == nil {
			return fmt.Errorf("helper %q has no value", name)
		}
	}
	return session.Export(HelperQualifier, helpers)
}

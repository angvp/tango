package shellcore

import (
	"fmt"
	"regexp"
	"strings"
)

// Limitation is a piece of Go the interpreter (Yaegi, pinned at v0.16.1)
// cannot run, which a person can reasonably type. The table of these is the
// single source for the hint printed after a failure, for help(), for
// `tango shell --help` and for the regression test that notices when a
// release of the interpreter starts supporting one. The guide's table must
// say the same.
type Limitation struct {
	// Feature names what does not work.
	Feature string
	// Workaround says what to write instead.
	Workaround string
	// Examples each fail today; Setup runs first, one chunk per element.
	Examples []Example
	// matches reports whether a failure of code is this limitation.
	matches func(code string, err error) bool
}

// Example is code that shows a Limitation.
type Example struct {
	Setup []string
	Code  string
}

var (
	undefinedBuiltin  = regexp.MustCompile(`undefined: (min|max|clear)\b`)
	genericInference  = regexp.MustCompile(`cannot use type func\(.*\) .* as type func\(.*\)`)
	rangeKeyword      = regexp.MustCompile(`\brange\b`)
	builtinNameInCode = func(name string) *regexp.Regexp { return regexp.MustCompile(`\b` + name + `\(`) }
)

// Limitations lists what is known not to run, in the order the help shows it.
var Limitations = []Limitation{
	{
		Feature:    "the min and max builtins",
		Workaround: "compare with if, or write a small function",
		Examples:   []Example{{Code: "min(1, 2)"}, {Code: "max(1, 2)"}},
		matches: func(code string, err error) bool {
			m := undefinedBuiltin.FindStringSubmatch(err.Error())
			return m != nil && (m[1] == "min" || m[1] == "max") && builtinNameInCode(m[1]).MatchString(code)
		},
	},
	{
		Feature:    "the clear builtin",
		Workaround: "delete the keys in a loop: for k := range m { delete(m, k) }",
		Examples:   []Example{{Setup: []string{`m := map[string]int{"a": 1}`}, Code: "clear(m)"}},
		matches: func(code string, err error) bool {
			m := undefinedBuiltin.FindStringSubmatch(err.Error())
			return m != nil && m[1] == "clear"
		},
	},
	{
		Feature:    "inferring the type arguments of a generic function whose result type differs from its argument types",
		Workaround: "give the type arguments explicitly, as in Map[int, string](xs, f)",
		Examples: []Example{{
			Setup: []string{"func Map[T, U any](xs []T, f func(T) U) []U { var out []U; for _, x := range xs { out = append(out, f(x)) }; return out }"},
			Code:  `Map([]int{1}, func(i int) string { return "x" })`,
		}},
		matches: func(code string, err error) bool { return genericInference.MatchString(err.Error()) },
	},
	{
		Feature:    "ranging over a function, or over an integer (for i := range 3)",
		Workaround: "use a counting for loop, or call the function with a callback",
		Examples: []Example{
			{Code: "for v := range func(yield func(int) bool) { yield(1) } { _ = v }"},
			{Code: "for i := range 3 { _ = i }"},
		},
		matches: func(code string, err error) bool {
			_, panicked := err.(*PanicError)
			return panicked && rangeKeyword.MatchString(code)
		},
	},
}

// hintFor returns the one-line hint for a failure of code, or "".
func hintFor(code string, err error) string {
	for _, l := range Limitations {
		if l.matches(code, err) {
			return fmt.Sprintf("not supported by this interpreter (%s): %s. See `tango shell --help`.", l.Feature, l.Workaround)
		}
	}
	if _, panicked := err.(*PanicError); panicked {
		return "the interpreter panicked on this line; the Go it was given may be outside what it supports. See `tango shell --help`."
	}
	return ""
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// limitationsTable is the "what the interpreter cannot run" text.
func limitationsTable() string {
	var b strings.Builder
	b.WriteString("What the interpreter cannot run (Yaegi v0.16.1 understands Go 1.21 and 1.22):\n")
	for _, l := range Limitations {
		fmt.Fprintf(&b, "  - %s: %s.\n", capitalize(l.Feature), l.Workaround)
	}
	b.WriteString("  Calls to your own functions that return several values show only the first;\n  assign them (n, err := f()) to see the rest.\n")
	return b.String()
}

// StartupPointer is the one line a session prints when it starts.
const StartupPointer = "Go console: help() lists the helpers and `tango shell --help` what the interpreter cannot run."

// Help is the text of help(): the helpers, the project's own helpers and the
// limits of the interpreter.
func Help(projectHelpers []string) string {
	var b strings.Builder
	b.WriteString(`Models are addressed as app.Model; rows are map[string]any keyed by field name.

  Models()                           every model
  Describe("app.Model")              a model's fields
  Get("app.Model", pk)               one row by primary key
  List("app.Model", query)           rows; query is an optional map[string]any with
                                     where (equality), order ("-ID" is descending), limit, offset
  Count("app.Model", query)          how many rows match
  Create("app.Model", row)           store a row; returns it with its primary key
  Update("app.Model", pk, changes)   change fields; returns the row
  Delete("app.Model", pk)            remove a row
  Context()                          the session's context.Context
  help()   exit()   quit()
`)
	if len(projectHelpers) > 0 {
		b.WriteString("\nThis project's helpers:\n")
		for _, name := range projectHelpers {
			fmt.Fprintf(&b, "  project.%s\n", name)
		}
	}
	b.WriteString("\nImport what you use from the standard library, as in Go: import \"strings\"\n\n")
	b.WriteString(limitationsTable())
	return b.String()
}

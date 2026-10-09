package shellcore

import (
	"strings"
	"testing"
)

// Every documented limitation is run for real. When a release of the
// interpreter starts supporting one of these, this test fails until the
// Limitations table, the guide and this example change together.
func TestEveryDocumentedLimitationStillFails(t *testing.T) {
	for _, l := range Limitations {
		for _, ex := range l.Examples {
			t.Run(l.Feature+": "+ex.Code, func(t *testing.T) {
				s := newTestSession(t)
				for _, setup := range ex.Setup {
					if _, err := s.Eval(setup); err != nil {
						t.Fatalf("setup %q: %v", setup, err)
					}
				}
				_, err := s.Eval(ex.Code)
				if err == nil {
					t.Fatalf("%q now works: the interpreter supports %s. Remove it from Limitations and the guide's table, and delete this example.", ex.Code, l.Feature)
				}
				if !l.matches(ex.Code, err) {
					t.Fatalf("%q failed with %q, which the %q entry does not recognise", ex.Code, err, l.Feature)
				}
				hint := hintFor(ex.Code, err)
				if !strings.Contains(hint, l.Workaround) || !strings.Contains(hint, "tango shell --help") {
					t.Fatalf("hint = %q, want the workaround %q and the pointer to --help", hint, l.Workaround)
				}
			})
		}
	}
}

func TestFailuresThatAreNotLimitationsGetNoHint(t *testing.T) {
	s := newTestSession(t)
	for _, code := range []string{"undefinedName", "minimum(1)", "1 +"} {
		_, err := s.Eval(code)
		if err == nil {
			t.Fatalf("%q unexpectedly worked", code)
		}
		if hint := hintFor(code, err); hint != "" {
			t.Errorf("%q: hint = %q, want none for an ordinary error", code, hint)
		}
	}
}

func TestAnUnknownPanicStillPointsToTheLimits(t *testing.T) {
	hint := hintFor("whatever()", &PanicError{Value: "boom"})
	if !strings.Contains(hint, "panicked") || !strings.Contains(hint, "tango shell --help") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestSupportedGenericsAndOrdinaryCodeRunWithoutAHint(t *testing.T) {
	s := newTestSession(t)
	for _, code := range []string{
		"func Id[T any](x T) T { return x }",
		"Id(3)",
		`for i := 0; i < 3; i++ { _ = i }`,
	} {
		if _, err := s.Eval(code); err != nil {
			t.Fatalf("%q: %v", code, err)
		}
	}
}

func TestAFailureInASessionPrintsTheErrorThenOneHint(t *testing.T) {
	code, out, errOut := session(t, "min(1, 2)\n")
	if code != 1 || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "error: ") || !strings.HasPrefix(lines[1], "hint: not supported by this interpreter (the min and max builtins)") {
		t.Fatalf("stderr = %q, want an error line followed by exactly one hint line", errOut)
	}
}

func TestHelpAndUsageShareTheLimitationsTable(t *testing.T) {
	table := limitationsTable()
	if !strings.Contains(table, "Yaegi v0.16.1") {
		t.Fatalf("the table does not name the interpreter version:\n%s", table)
	}
	for _, l := range Limitations {
		if !strings.Contains(table, l.Workaround) {
			t.Errorf("the table is missing the entry for %s", l.Feature)
		}
	}
	_, helpOut, _ := session(t, "help()\n")
	_, usageOut, _ := session(t, "", "--help")
	for name, text := range map[string]string{"help()": helpOut, "--help": usageOut} {
		if !strings.Contains(text, table) {
			t.Errorf("%s does not contain the shared limitations table:\n%s", name, text)
		}
	}
	for _, verb := range verbNames {
		if !strings.Contains(helpOut, verb) {
			t.Errorf("help() does not mention %s", verb)
		}
	}
}

func TestHelpListsTheProjectHelpers(t *testing.T) {
	if got := Help(nil); strings.Contains(got, "project.") {
		t.Errorf("help() lists project helpers when there are none:\n%s", got)
	}
	got := Help([]string{"Backfill", "Reindex"})
	if !strings.Contains(got, "project.Backfill\n  project.Reindex\n") {
		t.Errorf("help() = %q, want the sorted helper names", got)
	}
}

package shellcore

import (
	"errors"
	"strings"
	"testing"
)

func runWithHelpers(t *testing.T, helpers map[string]any, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	boot := bootProject(t)
	boot.Helpers = helpers
	return runProject(t, boot, stdin, args...)
}

var sampleHelpers = map[string]any{
	"Greeting": func() string { return "hello" },
	"Stats":    func() map[string]int { return map[string]int{"posts": 2, "drafts": 1} },
	"Count":    func() (int, error) { return 5, nil },
	"Locked":   func() error { return errors.New("index is locked") },
	"Boom":     func() { panic("kaboom") },
	"Version":  "1.2",
	// A helper named like a built-in verb is only reachable as project.List.
	"List": func() string { return "the project's list" },
}

func TestHelpersAreCalledThroughTheProjectQualifier(t *testing.T) {
	script := strings.Join([]string{
		`project.Greeting()`,
		`project.Stats()`,
		`project.Count()`,
		`project.Version`,
		`project.List()`,
		`Count("blog.Post")`,
	}, "\n")
	code, out, errOut := runWithHelpers(t, sampleHelpers, script)
	want := "\"hello\"\nmap[drafts:1 posts:2]\n5\n\"1.2\"\n\"the project's list\"\n0\n"
	if code != 0 || out != want {
		t.Fatalf("code=%d\nstdout:\n%s\nwant:\n%s\nstderr: %s", code, out, want, errOut)
	}
}

func TestAHelperThatReturnsAnErrorFailsTheLine(t *testing.T) {
	code, out, errOut := runWithHelpers(t, sampleHelpers, "project.Locked()\n1\n")
	if code != 1 || out != "" || !strings.Contains(errOut, "error: index is locked") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestAHelperThatPanicsIsAPanicOfTheProgramNotTheInterpreter(t *testing.T) {
	code, _, errOut := runWithHelpers(t, sampleHelpers, "project.Boom()\n")
	if code != 1 || !strings.Contains(errOut, "panic: kaboom") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if strings.Contains(errOut, "hint:") {
		t.Fatalf("stderr = %q: a helper's own panic is not a limit of the interpreter", errOut)
	}
}

func TestAHelperPanicLeavesAnInteractiveSessionUsable(t *testing.T) {
	boot := bootProject(t)
	boot.Helpers = sampleHelpers
	session, err := NewSession(strings.NewReader(""), nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExportHelpers(session, boot.Helpers); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Eval("project.Boom()"); err == nil {
		t.Fatal("want the panic")
	}
	if got, err := session.Eval("project.Greeting()"); err != nil || got != `"hello"` {
		t.Fatalf("after the panic: %q, %v", got, err)
	}
}

func TestHelperNamesMustBeExportedGoIdentifiers(t *testing.T) {
	for _, name := range []string{"reindex", "Re index", "", "9Lives", "Re-index", "_Hidden"} {
		code, out, errOut := runWithHelpers(t, map[string]any{name: func() {}}, "1\n")
		if code != 1 || out != "" || !strings.Contains(errOut, "invalid helper name") {
			t.Errorf("%q: code=%d out=%q err=%q, want startup to fail before anything runs", name, code, out, errOut)
		}
	}
	code, _, errOut := runWithHelpers(t, map[string]any{"Nothing": nil}, "1\n")
	if code != 1 || !strings.Contains(errOut, `helper "Nothing" has no value`) {
		t.Errorf("nil helper: code=%d err=%q", code, errOut)
	}
}

func TestHelpListsTheRegisteredNamesSorted(t *testing.T) {
	_, out, _ := runWithHelpers(t, map[string]any{"Reindex": func() {}, "Backfill": func() {}}, "help()\n")
	if !strings.Contains(out, "project.Backfill\n  project.Reindex\n") {
		t.Fatalf("help() = %q, want the helper names sorted", out)
	}
}

type opaque struct{ N int }

// A value of a project type reaches the session. What can be done with it
// there is not promised; this only checks that it does not break the session.
func TestAProjectTypeCrossingTheBoundaryDoesNotBreakTheSession(t *testing.T) {
	code, out, errOut := runWithHelpers(t, map[string]any{"Make": func() *opaque { return &opaque{N: 4} }}, "o := project.Make()\n1 + 1\n")
	if code != 0 || out != "2\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

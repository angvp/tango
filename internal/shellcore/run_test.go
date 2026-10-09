package shellcore

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// session runs the shell with args and stdin and returns what it printed.
func session(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, Boot{}, args)
	return code, out.String(), errOut.String()
}

func TestDashCEvaluatesAnExpressionAndPrintsIt(t *testing.T) {
	code, out, errOut := session(t, "", "-c", "1 + 2")
	if code != 0 || out != "3\n" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q, want 0, \"3\\n\", \"\"", code, out, errOut)
	}
}

func TestVariablesPersistAcrossPipedLines(t *testing.T) {
	code, out, _ := session(t, "n := 40\nn + 2\n")
	if code != 0 || out != "42\n" {
		t.Fatalf("code=%d out=%q, want 0 and 42", code, out)
	}
}

func TestDeclarationsAssignmentsAndNilPrintNothing(t *testing.T) {
	script := "x := 1\nx = 2\nvar e error\ne\nimport \"strings\"\nfunc double(n int) int { return n * 2 }\ntype T struct{ A int }\n"
	code, out, errOut := session(t, script)
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q, want nothing printed", code, out, errOut)
	}
}

func TestMultilineStatementsContinueWhileBracketsAreOpen(t *testing.T) {
	script := "func add(a, b int) int {\n\treturn a + b\n}\nadd(2, 3)\n"
	code, out, errOut := session(t, script)
	if code != 0 || out != "5\n" {
		t.Fatalf("code=%d out=%q err=%q, want 0 and 5", code, out, errOut)
	}
}

func TestFmtPrintGoesToStdout(t *testing.T) {
	code, out, _ := session(t, "import \"fmt\"\nfmt.Println(\"hi\")\n")
	if code != 0 || out != "hi\n" {
		t.Fatalf("code=%d out=%q, want hi", code, out)
	}
}

func TestAnErrorPrintsAndStopsNonInteractiveInput(t *testing.T) {
	code, out, errOut := session(t, "undefinedThing\n1 + 1\n")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.HasPrefix(errOut, "error: ") || !strings.Contains(errOut, "undefined") {
		t.Fatalf("stderr = %q, want an error: line naming the undefined symbol", errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want the line after the error not to run", out)
	}
}

// Yaegi v0.16.1 panics on a range over a function; the session survives it.
func TestAPanicIsRecoveredAndTheSessionKeepsWorking(t *testing.T) {
	var out, errOut bytes.Buffer
	s, err := NewSession(strings.NewReader(""), &out, &errOut, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Eval("for v := range func(yield func(int) bool) { yield(1) } { _ = v }")
	var p *PanicError
	if err == nil || !asPanic(err, &p) {
		t.Fatalf("Eval error = %v, want a recovered *PanicError", err)
	}
	got, err := s.Eval("1 + 1")
	if err != nil || got != "2" {
		t.Fatalf("after the panic Eval = %q, %v, want 2", got, err)
	}
}

func asPanic(err error, target **PanicError) bool {
	p, ok := err.(*PanicError)
	*target = p
	return ok
}

func TestExitAndQuitEndTheSessionCleanly(t *testing.T) {
	for _, word := range []string{"exit()", "quit()"} {
		code, out, _ := session(t, "1\n"+word+"\n2\n")
		if code != 0 || out != "1\n" {
			t.Fatalf("%s: code=%d out=%q, want 0 and only the first result", word, code, out)
		}
	}
}

func TestUnfinishedStatementAtEndOfInputIsAnError(t *testing.T) {
	code, _, errOut := session(t, "func f() {\n")
	if code != 1 || !strings.Contains(errOut, "unfinished statement") {
		t.Fatalf("code=%d err=%q, want 1 and an unfinished-statement error", code, errOut)
	}
}

func TestHelpPrintsUsageAndExitsZero(t *testing.T) {
	for _, flag := range []string{"--help", "-h", "-help"} {
		code, out, _ := session(t, "", flag)
		if code != 0 || !strings.Contains(out, "usage: tango shell") {
			t.Fatalf("%s: code=%d out=%q, want usage", flag, code, out)
		}
	}
}

func TestBadArgumentsAreAUsageError(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"stray"}, {"-c"}} {
		code, out, errOut := session(t, "", args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "usage: tango shell") {
			t.Fatalf("%v: code=%d out=%q err=%q, want 2 and usage on stderr", args, code, out, errOut)
		}
	}
}

func TestTheDatabaseLabelGoesToStderrNotStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, Boot{DatabaseLabel: "sqlite: app.db"}, []string{"-c", "1"})
	if code != 0 || out.String() != "1\n" || !strings.Contains(errOut.String(), "database: sqlite: app.db") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestReadOnlyFlagIsAccepted(t *testing.T) {
	inv, err := ParseArgs([]string{"--readonly", "-c", "1"})
	if err != nil || !inv.ReadOnly || !inv.HasEval || inv.Eval != "1" {
		t.Fatalf("inv=%+v err=%v", inv, err)
	}
}

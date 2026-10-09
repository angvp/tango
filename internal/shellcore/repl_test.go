package shellcore

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

type keystroke struct {
	line      string
	interrupt bool
	eof       bool
}

// scripted plays keystrokes back and records the prompts it was shown.
type scripted struct {
	keys    []keystroke
	prompts []string
}

func (s *scripted) ReadLine(prompt string) (string, bool, error) {
	s.prompts = append(s.prompts, prompt)
	if len(s.keys) == 0 {
		return "", false, io.EOF
	}
	k := s.keys[0]
	s.keys = s.keys[1:]
	switch {
	case k.interrupt:
		return "", true, nil
	case k.eof:
		return "", false, io.EOF
	}
	return k.line, false, nil
}

func playRepl(t *testing.T, keys ...keystroke) (code int, out, errOut string, prompts []string, evaluations [][]bool) {
	t.Helper()
	var o, e bytes.Buffer
	streams := IO{In: strings.NewReader(""), Out: &o, Err: &e}
	session, err := NewSession(streams.In, &o, &e, 200)
	if err != nil {
		t.Fatal(err)
	}
	src := &scripted{keys: keys}
	var states []bool
	loop := &repl{source: src, session: session, streams: streams, evaluating: func(on bool) { states = append(states, on) }}
	code = loop.run()
	_ = context.Background()
	return code, o.String(), e.String(), src.prompts, [][]bool{states}
}

func typed(lines ...string) []keystroke {
	keys := make([]keystroke, len(lines))
	for i, l := range lines {
		keys[i] = keystroke{line: l}
	}
	return keys
}

func TestReplKeepsGoingAfterAnErrorAndAPanic(t *testing.T) {
	keys := append(typed(
		"undefinedName",
		"for v := range func(yield func(int) bool) { yield(1) } { _ = v }",
		"1 + 1",
	), keystroke{eof: true})
	code, out, errOut, _, _ := playRepl(t, keys...)
	if code != 0 || out != "2\n\n" {
		t.Fatalf("code=%d out=%q, want the later line to run and Ctrl-D to leave cleanly", code, out)
	}
	if !strings.Contains(errOut, "error: ") || !strings.Contains(errOut, "panic: ") || strings.Count(errOut, "hint: ") != 1 {
		t.Fatalf("stderr = %q, want the error, the panic and the range hint", errOut)
	}
}

func TestReplContinuesAMultilineStatementWithADifferentPrompt(t *testing.T) {
	_, out, _, prompts, _ := playRepl(t, append(typed("func add(a, b int) int {", "return a + b", "}", "add(2, 3)"), keystroke{eof: true})...)
	if want := []string{promptNew, promptContinue, promptContinue, promptNew, promptNew}; strings.Join(prompts, "|") != strings.Join(want, "|") {
		t.Fatalf("prompts = %q, want %q", prompts, want)
	}
	if !strings.HasPrefix(out, "5\n") {
		t.Fatalf("out = %q, want 5", out)
	}
}

func TestReplExitQuitAndEOFLeaveCleanly(t *testing.T) {
	for _, word := range []string{"exit()", "quit()"} {
		code, out, _, _, _ := playRepl(t, typed(word, "1 + 1")...)
		if code != 0 || out != "" {
			t.Errorf("%s: code=%d out=%q, want a clean exit before anything after it runs", word, code, out)
		}
	}
	code, out, _, _, _ := playRepl(t, keystroke{eof: true})
	if code != 0 || out != "\n" {
		t.Errorf("Ctrl-D: code=%d out=%q, want a clean exit that ends the prompt line", code, out)
	}
}

func TestOneCtrlCClearsTheLineAndTwoInARowLeave(t *testing.T) {
	code, out, _, prompts, _ := playRepl(t, keystroke{interrupt: true}, keystroke{line: "1 + 1"}, keystroke{interrupt: true}, keystroke{interrupt: true}, keystroke{line: "never"})
	if code != 0 {
		t.Fatalf("code = %d, want a clean exit on the second Ctrl-C", code)
	}
	if strings.Contains(out, "never") || strings.Count(out, "press Ctrl-C again") != 2 || !strings.Contains(out, "2\n") {
		t.Fatalf("out = %q: the first Ctrl-C only clears (and says how to leave), a line in between resets the count", out)
	}
	if len(prompts) != 4 {
		t.Fatalf("prompts = %q, want the loop to stop after the second Ctrl-C in a row", prompts)
	}
}

func TestCtrlCDropsAHalfTypedMultilineStatement(t *testing.T) {
	_, out, _, prompts, _ := playRepl(t, keystroke{line: "func f() {"}, keystroke{interrupt: true}, keystroke{line: "7"}, keystroke{eof: true})
	if !strings.Contains(out, "7\n") {
		t.Fatalf("out = %q, want the statement after Ctrl-C to run on its own", out)
	}
	if prompts[1] != promptContinue || prompts[2] != promptNew {
		t.Fatalf("prompts = %q, want the continuation prompt to reset after Ctrl-C", prompts)
	}
}

func TestReplTellsTheTerminalWhenAnEvaluationRuns(t *testing.T) {
	_, _, _, _, states := playRepl(t, append(typed("1 + 1", "help()"), keystroke{eof: true})...)
	// help() is answered by the shell, not evaluated.
	if got := states[0]; len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("evaluation states = %v, want one start and one end", got)
	}
}

func TestReplHelpPrintsTheHelpText(t *testing.T) {
	_, out, _, _, _ := playRepl(t, append(typed("help()"), keystroke{eof: true})...)
	if !strings.Contains(out, "What the interpreter cannot run") {
		t.Fatalf("out = %q", out)
	}
}

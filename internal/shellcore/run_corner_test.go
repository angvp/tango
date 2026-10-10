package shellcore

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestBalancedLeavesAMalformedStringToTheInterpreter(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{`x := "never closed`, true},          // an unterminated interpreted string is reported by the interpreter
		{"x := \"line one\nline two\"", true}, // a newline inside "..." ends the string early
		{"x := `raw\nstill raw", false},       // a raw string may span lines, so wait for its end
		{"x := `raw\nends`", true},
		{`x := 'a`, true},
		{`f("a\"b"`, false},
	}
	for _, tt := range tests {
		if got := Balanced(tt.code); got != tt.want {
			t.Errorf("Balanced(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestCapitalizeLeavesAnEmptyStringAlone(t *testing.T) {
	if got := capitalize(""); got != "" {
		t.Fatalf("capitalize(\"\") = %q", got)
	}
	if got := capitalize("limit"); got != "Limit" {
		t.Fatalf("capitalize(limit) = %q", got)
	}
}

func TestACancelledContextStopsAScriptBeforeItsFirstStatement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	code := Run(ctx, IO{In: strings.NewReader("1 + 1\n"), Out: &out, Err: &errOut}, Boot{}, nil)
	if code != ExitError || out.Len() != 0 {
		t.Fatalf("code %d, stdout %q; want ExitError and no output", code, out.String())
	}
}

func TestAScriptThatEndsInsideAStatementFailsWithAClearMessage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), IO{In: strings.NewReader("func f() {\n"), Out: &out, Err: &errOut}, Boot{}, nil)
	if code != ExitError || !strings.Contains(errOut.String(), "unfinished statement") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestHelpIsPrintedForBothSpellingsAndAnUnknownFlagIsAUsageError(t *testing.T) {
	for _, flagName := range []string{"-h", "-help", "--help"} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, Boot{}, []string{flagName}); code != ExitOK || !strings.Contains(out.String(), "Usage") && !strings.Contains(out.String(), "usage") {
			t.Errorf("%s: code %d, stdout %q", flagName, code, out.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, Boot{}, []string{"--nope"}); code != ExitUsage || !strings.Contains(errOut.String(), "nope") {
		t.Errorf("--nope: code %d, stderr %q", code, errOut.String())
	}
}

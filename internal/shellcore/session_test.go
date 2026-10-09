package shellcore

import (
	"errors"
	"strings"
	"testing"
)

func newTestSession(t *testing.T) *Session {
	t.Helper()
	s, err := NewSession(strings.NewReader(""), nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExportedBareFunctionsShowEveryValueTheyReturn(t *testing.T) {
	s := newTestSession(t)
	if err := s.Export("", map[string]any{
		"Count":   func() (int, error) { return 7, nil },
		"Fail":    func() (int, error) { return 7, errors.New("no such model") },
		"Only":    func() error { return errors.New("refused") },
		"Fine":    func() error { return nil },
		"Rows":    func() ([]map[string]any, error) { return []map[string]any{{"ID": 1}, {"ID": 2}}, nil },
		"Twice":   func() (string, int) { return "a", 2 },
		"Nothing": func() {},
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		code, want, wantErr string
	}{
		{"Count()", "7", ""},
		{"Fail()", "", "no such model"},
		{"Only()", "", "refused"},
		{"Fine()", "", ""},
		{"Rows()", "map[ID:1]\nmap[ID:2]", ""},
		{"Twice()", "\"a\"\n2", ""},
		{"Nothing()", "", ""},
		{"x := 1\nCount()", "7", ""},
	}
	for _, tt := range tests {
		got, err := s.Eval(tt.code)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: err = %v, want %q", tt.code, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s: got %q, %v, want %q", tt.code, got, err, tt.want)
		}
	}
}

func TestExportedQualifiedFunctionsAreCalledThroughTheirQualifier(t *testing.T) {
	s := newTestSession(t)
	if err := s.Export("project", map[string]any{
		"Reindex": func() (int, error) { return 3, errors.New("index is locked") },
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Eval("project.Reindex()"); err == nil || !strings.Contains(err.Error(), "index is locked") {
		t.Fatalf("err = %v, want the helper's error", err)
	}
	if _, err := s.Eval("Reindex()"); err == nil {
		t.Fatal("Reindex must not be callable without the project. qualifier")
	}
}

func TestResultsOfAMultiValueCallAreStillAssignable(t *testing.T) {
	s := newTestSession(t)
	if err := s.Export("", map[string]any{"Pair": func() (int, error) { return 7, errors.New("bad") }}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Eval("n, err := Pair()"); err != nil {
		t.Fatalf("assigning both values failed: %v", err)
	}
	if got, err := s.Eval("n"); err != nil || got != "7" {
		t.Fatalf("n = %q, %v, want 7", got, err)
	}
}

func TestPrintingFunctionsDoNotPrintTheirByteCount(t *testing.T) {
	var out strings.Builder
	s, err := NewSession(strings.NewReader(""), &out, &out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Eval(`import "fmt"`); err != nil {
		t.Fatal(err)
	}
	printed, err := s.Eval(`fmt.Println("hi")`)
	if err != nil || printed != "" || out.String() != "hi\n" {
		t.Fatalf("printed=%q err=%v out=%q, want only the program's own output", printed, err, out.String())
	}
}

func TestAnExpressionHoldingAnErrorIsAnError(t *testing.T) {
	s := newTestSession(t)
	if _, err := s.Eval(`import "errors"`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Eval(`failure := errors.New("boom")`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Eval(`failure`); err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}

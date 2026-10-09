package admin

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeTerminal swaps the terminal operations for the test. The real ones are
// a thin wrapper over golang.org/x/term, the only piece left untested.
func fakeTerminal(t *testing.T, isTerminal bool, read func(fd int) ([]byte, error)) {
	t.Helper()
	saved := terminal
	t.Cleanup(func() { terminal = saved })
	terminal = terminalOps{
		isTerminal:   func(int) bool { return isTerminal },
		readPassword: read,
	}
}

func stdinFile(t *testing.T, content string) *os.File {
	t.Helper()
	path := t.TempDir() + "/stdin"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestReadPasswordOnATerminalReadsWithoutEcho(t *testing.T) {
	stdin := stdinFile(t, "must-not-be-read\n")
	var gotFD int
	fakeTerminal(t, true, func(fd int) ([]byte, error) { gotFD = fd; return []byte("typed-secret"), nil })
	var stdout strings.Builder

	got, err := readPassword(stdin, &stdout, "New password: ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "typed-secret" || gotFD != int(stdin.Fd()) {
		t.Fatalf("password=%q fd=%d, want typed-secret on stdin's descriptor %d", got, gotFD, stdin.Fd())
	}
	// With echo off the Enter key is not displayed, so the prompt line
	// must be ended explicitly.
	if want := "New password: \n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestReadPasswordOnATerminalReportsReadErrors(t *testing.T) {
	boom := errors.New("terminal went away")
	fakeTerminal(t, true, func(int) ([]byte, error) { return nil, boom })

	if _, err := readPassword(stdinFile(t, ""), &strings.Builder{}, "New password: "); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestReadPasswordFromAPipeOrFileIsUnchanged(t *testing.T) {
	fakeTerminal(t, false, func(int) ([]byte, error) { t.Fatal("echo handling used for a non-terminal"); return nil, nil })
	var stdout strings.Builder

	got, err := readPassword(stdinFile(t, "piped\n"), &stdout, "New password: ")
	if err != nil || got != "piped" {
		t.Fatalf("password=%q err=%v, want piped", got, err)
	}
	if stdout.String() != "New password: " {
		t.Fatalf("stdout = %q, want only the prompt", stdout.String())
	}
}

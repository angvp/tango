//go:build linux || darwin

package shellcore

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// terminalSession runs the real interactive shell on a pseudo-terminal and
// lets a test type at it.
type terminalSession struct {
	t      *testing.T
	master io.Writer
	mu     sync.Mutex
	screen bytes.Buffer
	exit   chan int
	errOut *bytes.Buffer
}

func startTerminalSession(t *testing.T) *terminalSession {
	t.Helper()
	t.Setenv("TANGO_SHELL_HISTORY", "off")
	master, slave := openPTY(t)
	ts := &terminalSession{t: t, master: master, exit: make(chan int, 1), errOut: &bytes.Buffer{}}
	t.Cleanup(func() { master.Close(); slave.Close() })

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			ts.mu.Lock()
			ts.screen.Write(buf[:n])
			ts.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		ts.exit <- Run(context.Background(), IO{In: slave, Out: slave, Err: ts.errOut}, Boot{}, nil)
	}()
	return ts
}

func (ts *terminalSession) type_(text string) {
	ts.t.Helper()
	if _, err := io.WriteString(ts.master, text); err != nil {
		ts.t.Fatal(err)
	}
}

func (ts *terminalSession) waitFor(text string) {
	ts.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ts.mu.Lock()
		seen := strings.Contains(ts.screen.String(), text)
		ts.mu.Unlock()
		if seen {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.t.Fatalf("never saw %q on the terminal; screen:\n%q", text, ts.screen.String())
}

// waitPrompt waits until the shell has shown its main prompt n times. Typing
// before then can reach the terminal while it is not yet in raw mode, where
// Ctrl-C and Ctrl-D never arrive as bytes.
func (ts *terminalSession) waitPrompt(n int) {
	ts.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ts.mu.Lock()
		seen := strings.Count(ts.screen.String(), promptNew)
		ts.mu.Unlock()
		if seen >= n {
			time.Sleep(50 * time.Millisecond) // let the editor enter raw mode
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	ts.t.Fatalf("the prompt was shown fewer than %d times", n)
}

func (ts *terminalSession) waitExit() int {
	ts.t.Helper()
	select {
	case code := <-ts.exit:
		return code
	case <-time.After(10 * time.Second):
		ts.t.Fatal("the shell did not exit")
		return -1
	}
}

func TestAnInteractiveSessionEvaluatesALineAndLeavesOnCtrlD(t *testing.T) {
	ts := startTerminalSession(t)
	ts.waitFor(StartupPointer)
	ts.waitPrompt(1)
	ts.type_("6 * 7\r")
	ts.waitFor("42")
	ts.waitPrompt(2)
	ts.type_("\x04") // Ctrl-D on an empty line
	if code := ts.waitExit(); code != ExitOK {
		t.Fatalf("exit code %d, want %d", code, ExitOK)
	}
}

func TestInteractiveStatementsContinueAcrossLinesAndErrorsKeepTheSessionGoing(t *testing.T) {
	ts := startTerminalSession(t)
	ts.waitFor(promptNew)
	ts.type_("x := []int{1,\r")
	ts.waitFor(promptContinue)
	ts.type_("2, 3}\r")
	ts.type_("len(x)\r")
	ts.waitFor("3")
	ts.type_("undefinedThing\r")
	ts.waitFor("tango> undefinedThing")
	ts.type_("help()\r")
	ts.waitFor("help")
	ts.type_("exit()\r")
	if code := ts.waitExit(); code != ExitOK {
		t.Fatalf("exit code %d, want %d", code, ExitOK)
	}
	if !strings.Contains(ts.errOut.String(), "undefinedThing") {
		t.Fatalf("the evaluation error did not reach stderr: %q", ts.errOut.String())
	}
}

func TestCtrlCClearsTheLineAndASecondOneInARowLeaves(t *testing.T) {
	ts := startTerminalSession(t)
	ts.waitPrompt(1)
	ts.type_("half a line\x03")
	ts.waitFor("(press Ctrl-C again, or Ctrl-D, to exit)")
	ts.waitPrompt(2)
	ts.type_("1 + 1\r") // a line in between resets the count
	ts.waitFor("2")
	ts.waitPrompt(3)
	ts.type_("\x03")
	ts.waitFor("(press Ctrl-C again, or Ctrl-D, to exit)\r\ntango> ")
	ts.waitPrompt(4)
	ts.type_("\x03")
	if code := ts.waitExit(); code != ExitOK {
		t.Fatalf("exit code %d, want %d", code, ExitOK)
	}
}

func TestCtrlCReaderNotesTheInterruptByteAndPassesBytesThrough(t *testing.T) {
	r := &ctrlCReader{r: strings.NewReader("ab\x03cd")}
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil || string(buf[:n]) != "ab\x03cd" || !r.seen {
		t.Fatalf("Read = %q, %v, seen %v", buf[:n], err, r.seen)
	}
	plain := &ctrlCReader{r: strings.NewReader("no interrupt")}
	if _, err := plain.Read(buf); err != nil || plain.seen {
		t.Fatalf("seen = %v for a read without Ctrl-C (err %v)", plain.seen, err)
	}
}

func TestIsTerminalIsFalseForNonFilesAndPipes(t *testing.T) {
	if isTerminal(strings.NewReader("x")) {
		t.Fatal("a strings.Reader is not a terminal")
	}
	master, slave := openPTY(t)
	defer master.Close()
	defer slave.Close()
	if !isTerminal(slave) {
		t.Fatal("the slave end of a pty is a terminal")
	}
}

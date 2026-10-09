package shellcore

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"

	"golang.org/x/term"
)

const (
	// ExitInterrupted is the exit status of a session that Ctrl-C ended in
	// the middle of an evaluation.
	ExitInterrupted = 130
	// interactiveLimit is how many characters one printed value may span at
	// a terminal before it is cut with an ellipsis.
	interactiveLimit = 200
	ctrlC            = 3
)

// exitOnInterrupt ends the process with status 130 when Ctrl-C (SIGINT)
// arrives during a non-interactive run, as it does during an evaluation at
// the prompt: the interpreter cannot interrupt a running evaluation. The
// returned func removes the handler.
func exitOnInterrupt(errOut io.Writer) (stop func()) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-interrupts:
			fmt.Fprintln(errOut)
			os.Exit(ExitInterrupted)
		case <-done:
		}
	}()
	return func() { signal.Stop(interrupts); close(done) }
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// runInteractive is the session at a terminal: line editing, history, and the
// Ctrl-C rules. Ctrl-C at the prompt clears the line and a second one in a
// row leaves; Ctrl-C during an evaluation ends the process with status 130,
// because the interpreter cannot interrupt a running evaluation.
func runInteractive(session *Session, s IO, helperNames []string) int {
	in, out := s.In.(*os.File), s.Out.(*os.File)

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(s.Err, "tango shell: history is not saved: %v\n", err)
		dir = ""
	}
	history := OpenHistory(dir, s.Err)
	source := &termSource{in: in, out: out, history: history}

	var evaluating atomic.Bool
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	done := make(chan struct{})
	defer func() { signal.Stop(interrupts); close(done) }()
	go func() {
		for {
			select {
			case <-interrupts:
				if evaluating.Load() {
					fmt.Fprintln(s.Err)
					os.Exit(ExitInterrupted)
				}
			case <-done:
				return
			}
		}
	}()

	fmt.Fprintln(s.Out, StartupPointer)
	loop := &repl{
		source:      source,
		session:     session,
		streams:     s,
		helperNames: helperNames,
		evaluating:  evaluating.Store,
	}
	return loop.run()
}

// termSource reads lines with golang.org/x/term's editor, putting the
// terminal in raw mode only while a line is being typed so that output, and
// Ctrl-C during an evaluation, behave normally the rest of the time.
type termSource struct {
	in, out  *os.File
	history  *History
	reader   *ctrlCReader
	terminal *term.Terminal
}

func (t *termSource) ReadLine(prompt string) (string, bool, error) {
	fd := int(t.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", false, err
	}
	defer func() {
		if err := term.Restore(fd, state); err != nil {
			fmt.Fprintf(t.out, "tango shell: could not restore the terminal: %v\r\n", err)
		}
	}()

	if t.terminal == nil {
		t.reader = &ctrlCReader{r: t.in}
		t.terminal = term.NewTerminal(struct {
			io.Reader
			io.Writer
		}{t.reader, t.out}, "")
		t.terminal.History = t.history
	}
	t.terminal.SetPrompt(prompt)
	t.reader.seen = false
	line, err := t.terminal.ReadLine()
	if err == io.EOF && t.reader.seen {
		// x/term reports Ctrl-C as it reports Ctrl-D. It keeps the
		// half-typed line, so start a fresh editor.
		t.terminal = nil
		fmt.Fprint(t.out, "^C\r\n")
		return "", true, nil
	}
	return line, false, err
}

// ctrlCReader notes whether the last read held a Ctrl-C byte.
type ctrlCReader struct {
	r    io.Reader
	seen bool
}

func (c *ctrlCReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if bytes.IndexByte(p[:n], ctrlC) >= 0 {
		c.seen = true
	}
	return n, err
}

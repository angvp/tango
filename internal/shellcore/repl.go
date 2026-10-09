package shellcore

import (
	"fmt"
	"io"
	"strings"
)

const (
	promptNew      = "tango> "
	promptContinue = "  ...> "
)

// lineSource is where an interactive session gets its lines.
type lineSource interface {
	// ReadLine shows prompt and returns the line typed. interrupted reports
	// that Ctrl-C was pressed, which discards whatever was typed; err is
	// io.EOF when the person pressed Ctrl-D on an empty line.
	ReadLine(prompt string) (line string, interrupted bool, err error)
}

// repl is the interactive read-eval-print loop over any lineSource.
type repl struct {
	source      lineSource
	session     *Session
	streams     IO
	helperNames []string
	// evaluating, when set, is told when an evaluation starts and ends, so
	// that Ctrl-C during one can end the process.
	evaluating func(bool)
}

// run reads lines until the person leaves, evaluating each complete
// statement. An error or panic is shown and the session goes on.
func (r *repl) run() int {
	var pending strings.Builder
	interrupts := 0
	for {
		prompt := promptNew
		if pending.Len() > 0 {
			prompt = promptContinue
		}
		line, interrupted, err := r.source.ReadLine(prompt)
		if interrupted {
			pending.Reset()
			interrupts++
			if interrupts >= 2 {
				fmt.Fprintln(r.streams.Out)
				return ExitOK
			}
			fmt.Fprintln(r.streams.Out, "(press Ctrl-C again, or Ctrl-D, to exit)")
			continue
		}
		interrupts = 0
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(r.streams.Out)
				return ExitOK
			}
			fmt.Fprintf(r.streams.Err, "tango shell: %v\n", err)
			return ExitError
		}
		if pending.Len() == 0 && strings.TrimSpace(line) == "" {
			continue
		}
		if pending.Len() > 0 {
			pending.WriteByte('\n')
		}
		pending.WriteString(line)
		if !Balanced(pending.String()) {
			continue
		}
		code := pending.String()
		pending.Reset()
		switch {
		case isExit(code):
			return ExitOK
		case strings.TrimSpace(code) == "help()":
			fmt.Fprint(r.streams.Out, Help(r.helperNames))
		default:
			if r.evaluating != nil {
				r.evaluating(true)
			}
			evalAndPrint(r.session, code, r.streams)
			if r.evaluating != nil {
				r.evaluating(false)
			}
		}
	}
}

package shellcore

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Invocation is what the command line of a shell session asks for.
type Invocation struct {
	// Eval holds the -c expression; HasEval distinguishes "-c ''" from no -c.
	Eval     string
	HasEval  bool
	ReadOnly bool
	Help     bool
}

// UsageError reports a command line the shell cannot accept; the caller
// exits with status 2.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

// ParseArgs reads the shell's command line: -c EXPR, --readonly, --help.
func ParseArgs(args []string) (Invocation, error) {
	var inv Invocation
	flags := flag.NewFlagSet("tango shell", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Func("c", "evaluate EXPR, print the result and exit", func(v string) error {
		inv.Eval, inv.HasEval = v, true
		return nil
	})
	flags.BoolVar(&inv.ReadOnly, "readonly", false, "refuse every helper that writes")
	flags.BoolVar(&inv.Help, "help", false, "print this help")
	flags.BoolVar(&inv.Help, "h", false, "print this help")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			inv.Help = true
			return inv, nil
		}
		return Invocation{}, &UsageError{Message: err.Error()}
	}
	if flags.NArg() > 0 {
		return Invocation{}, &UsageError{Message: fmt.Sprintf("unexpected argument %q", strings.Join(flags.Args(), " "))}
	}
	return inv, nil
}

// Usage is the text of `tango shell --help`.
const Usage = `usage: tango shell [-c EXPR] [--readonly]

  -c EXPR      evaluate EXPR, print the result and exit
  --readonly   refuse every helper that writes
  --help       print this help

With no -c and input from a pipe, each complete line is evaluated in turn
and the first error stops the run with a non-zero exit status.
`

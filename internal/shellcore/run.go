package shellcore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

// IO is where a shell session reads and writes.
type IO struct {
	In       io.Reader
	Out, Err io.Writer
}

// Boot is the booted project a session works on.
type Boot struct {
	Registry *tango.Registry
	Store    *db.Store

	ReadOnly      bool
	Helpers       map[string]any
	DatabaseLabel string
}

// Exit statuses of a session.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Run executes one shell session for args and returns the process exit code.
// Results go to s.Out; errors, the database line and everything else a
// script should not parse go to s.Err.
func Run(ctx context.Context, s IO, boot Boot, args []string) int {
	inv, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(s.Err, "tango shell: %v\n\n%s", err, Usage())
		return ExitUsage
	}
	if inv.Help {
		fmt.Fprint(s.Out, Usage())
		return ExitOK
	}
	boot.ReadOnly = boot.ReadOnly || inv.ReadOnly

	interactive := !inv.HasEval && isTerminal(s.In) && isTerminal(s.Out)
	limit := 0
	if interactive {
		limit = interactiveLimit
	}
	session, err := NewSession(s.In, s.Out, s.Err, limit)
	if err != nil {
		fmt.Fprintf(s.Err, "tango shell: %v\n", err)
		return ExitError
	}
	if boot.Registry != nil {
		verbs := NewVerbs(ctx, boot.Registry.Models(), boot.Store, boot.ReadOnly)
		if err := session.Export("", verbs.Functions()); err != nil {
			fmt.Fprintf(s.Err, "tango shell: %v\n", err)
			return ExitError
		}
	}
	if boot.DatabaseLabel != "" {
		fmt.Fprintf(s.Err, "database: %s\n", boot.DatabaseLabel)
	}

	if interactive {
		return runInteractive(session, s, boot, sortedKeys(boot.Helpers))
	}

	var input = s.In
	if inv.HasEval {
		input = strings.NewReader(inv.Eval)
	}
	return runLines(ctx, session, input, s, sortedKeys(boot.Helpers))
}

// runLines evaluates input a complete statement at a time and stops at the
// first error.
func runLines(ctx context.Context, session *Session, input io.Reader, s IO, helperNames []string) int {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var pending strings.Builder
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ExitError
		}
		line := scanner.Text()
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
		if isExit(code) {
			return ExitOK
		}
		if strings.TrimSpace(code) == "help()" {
			fmt.Fprint(s.Out, Help(helperNames))
			continue
		}
		if code := evalAndPrint(session, code, s); code != ExitOK {
			return code
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(s.Err, "error: %v\n", err)
		return ExitError
	}
	if pending.Len() > 0 {
		fmt.Fprintln(s.Err, "error: input ended inside an unfinished statement")
		return ExitError
	}
	return ExitOK
}

func isExit(code string) bool {
	switch strings.TrimSpace(code) {
	case "exit()", "quit()":
		return true
	}
	return false
}

// evalAndPrint evaluates one complete statement and reports its outcome.
func evalAndPrint(session *Session, code string, s IO) int {
	printed, err := session.Eval(code)
	if err != nil {
		var p *PanicError
		if errors.As(err, &p) {
			fmt.Fprintf(s.Err, "panic: %v\n", p.Value)
		} else {
			fmt.Fprintf(s.Err, "error: %v\n", err)
		}
		if hint := hintFor(code, err); hint != "" {
			fmt.Fprintf(s.Err, "hint: %s\n", hint)
		}
		return ExitError
	}
	if printed != "" {
		fmt.Fprintln(s.Out, printed)
	}
	return ExitOK
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

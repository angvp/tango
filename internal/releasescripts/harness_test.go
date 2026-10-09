// Package releasescripts tests the release shell scripts in scripts/ by
// running them with a stand-in for the GitHub CLI, so what a release job
// does when the website or GitHub misbehaves is checked without a release.
package releasescripts

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ghStub is a stand-in `gh`. Its behaviour comes from files in $STUB_DIR, and
// every call is appended to $STUB_DIR/calls.
const ghStub = `#!/bin/sh
d="$STUB_DIR"
echo "$*" >> "$d/calls"
case "$1 $2" in
"run list")
	if [ -f "$d/dispatched" ] && [ -f "$d/after" ]; then cat "$d/after"; else cat "$d/before"; fi ;;
"workflow run")
	if [ -f "$d/dispatch_fails" ]; then echo "HTTP 403: Resource not accessible" >&2; exit 1; fi
	touch "$d/dispatched" ;;
"run view")
	case "$*" in
	*url*) echo "https://example.test/runs/$3"; exit 0 ;;
	esac
	n=$(cat "$d/n" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "$d/n"
	total=$(wc -l < "$d/views")
	[ "$n" -gt "$total" ] && n=$total
	sed -n "${n}p" "$d/views" ;;
*) echo "unexpected gh call: $*" >&2; exit 2 ;;
esac
`

// scenario is what the stand-in gh answers.
type scenario struct {
	before       string   // run ids listed before the dispatch, one per line
	after        string   // run ids listed after it ("" means the new run never appears)
	views        []string // successive "status conclusion" answers for the new run
	dispatchFail bool
}

// result is one script run.
type result struct {
	out   string
	exit  int
	calls string
}

func runScript(t *testing.T, script string, sc scenario, env []string, args ...string) result {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(ghStub), 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "stub")
	if err := os.Mkdir(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(stub, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("before", sc.before)
	if sc.after != "" {
		write("after", sc.after)
	}
	write("views", strings.Join(sc.views, "\n")+"\n")
	if sc.dispatchFail {
		write("dispatch_fails", "")
	}

	abs, err := filepath.Abs(filepath.Join("..", "..", "scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{abs}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_DIR="+stub)
	cmd.Env = append(cmd.Env, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	exit := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(runErr, &exitErr):
		exit = exitErr.ExitCode()
	case runErr != nil:
		t.Fatal(runErr)
	}
	calls, _ := os.ReadFile(filepath.Join(stub, "calls"))
	return result{out: out.String(), exit: exit, calls: string(calls)}
}

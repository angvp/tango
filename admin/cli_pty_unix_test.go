//go:build unix

package admin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestReadPasswordHelperProcess is not a test: the pty test below re-runs the
// test binary to execute just this, with a terminal as its standard input.
func TestReadPasswordHelperProcess(t *testing.T) {
	if os.Getenv("ADMIN_PTY_HELPER") != "1" {
		t.Skip("run by TestPasswordPromptOnATerminal")
	}
	fmt.Print("READY\n") // the terminal turns \n into \r\n
	password, err := readPasswordNoEcho(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Printf("ERROR:%v\n", err)
		os.Exit(2)
	}
	fmt.Printf("READ:%s\n", password)
	os.Exit(0)
}

// TestPasswordPromptOnATerminal runs the real no-echo read on a pseudo-
// terminal: the typed password is not shown, and Ctrl-C ends the process with
// status 130 and the terminal's echo back on (the platform call the fake
// terminal in the other tests stands in for).
func TestPasswordPromptOnATerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a pseudo-terminal")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed; it drives the pseudo-terminal")
	}
	driver, err := filepath.Abs(filepath.Join("testdata", "password_pty_driver.py"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-I", driver, binary)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("terminal session failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

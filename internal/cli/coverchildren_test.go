package cli

import (
	"os"
	"os/exec"
	"testing"
)

// The tests that scaffold a project, build it and run the binary exercise
// framework code in a child process, which a plain coverage profile cannot
// see. When TANGO_COVERDIR names a directory, they build those programs with
// coverage instrumentation and hand the directory to them, so scripts/coverage.sh
// can add what they ran to the profile. Unset, nothing changes.
//
// `go test` replaces GOCOVERDIR for the test process, hence the other name.
const childCoverEnv = "TANGO_COVERDIR"

// goBuild returns a `go build` command; with child coverage on, the binary is
// instrumented for every package it links.
func goBuild(args ...string) *exec.Cmd {
	command := []string{"build"}
	if os.Getenv(childCoverEnv) != "" {
		command = append(command, "-cover", "-covermode=atomic", "-coverpkg=all")
	}
	return exec.Command("go", append(command, args...)...)
}

// withChildCover adds GOCOVERDIR to env when child coverage is on.
func withChildCover(env []string) []string {
	if dir := os.Getenv(childCoverEnv); dir != "" {
		return append(env, "GOCOVERDIR="+dir)
	}
	return env
}

func TestWithChildCoverAddsTheDirectoryOnlyWhenAsked(t *testing.T) {
	t.Setenv(childCoverEnv, "")
	if got := withChildCover([]string{"A=1"}); len(got) != 1 {
		t.Errorf("child coverage off: env = %v", got)
	}
	t.Setenv(childCoverEnv, "/tmp/cover")
	got := withChildCover([]string{"A=1"})
	if len(got) != 2 || got[1] != "GOCOVERDIR=/tmp/cover" {
		t.Errorf("child coverage on: env = %v", got)
	}
	if args := goBuild("-o", "x", ".").Args; len(args) != 8 || args[2] != "-cover" || args[4] != "-coverpkg=all" {
		t.Errorf("instrumented build = %v", args)
	}
}

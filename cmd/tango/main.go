package main

import (
	"context"
	"os"

	"github.com/angvp/tango/internal/cli"
)

// exit is os.Exit, a variable so a test can observe the exit code.
var exit = os.Exit

func main() {
	exit(cli.Run(context.Background(), os.Args[1:], ".", os.Stdout, os.Stderr, cli.ExecRunner{}))
}

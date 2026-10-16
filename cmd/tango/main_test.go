package main

import (
	"os"
	"testing"
)

func TestMainExitsWithTheCommandsStatus(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"help succeeds", []string{"tango", "help"}, 0},
		{"an unknown command fails", []string{"tango", "no-such-command"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldArgs, oldExit := os.Args, exit
			t.Cleanup(func() { os.Args, exit = oldArgs, oldExit })
			got := -1
			os.Args, exit = tc.args, func(code int) { got = code }
			main()
			if got != tc.want {
				t.Fatalf("exit code = %d, want %d", got, tc.want)
			}
		})
	}
}

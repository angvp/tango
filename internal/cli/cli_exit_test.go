package cli

import (
	"context"
	"io"
	"testing"
)

// The migration commands pass through the exit code of the command they run;
// `tango tui` must not change that.
func TestMigrateCommandsKeepTheExitCodeOfTheCommandTheyRun(t *testing.T) {
	for _, args := range [][]string{{"migrate"}, {"migrate", "down"}} {
		runner := &multiRecordingRunner{err: exitError(t, 3)}
		if code := Run(context.Background(), args, t.TempDir(), io.Discard, io.Discard, runner); code != 3 {
			t.Errorf("tango %v exit code = %d, want 3", args, code)
		}
	}
}

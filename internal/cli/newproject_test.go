package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type multiRecordingRunner struct {
	commands []recordedCommand
	err      error
}

func (r *multiRecordingRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	r.commands = append(r.commands, recordedCommand{
		dir:  dir,
		name: name,
		args: append([]string(nil), args...),
	})
	return r.err
}

func TestNewProjectCreatesRunnableSQLiteWiredProject(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"newproject", "myapp"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	projectDir := filepath.Join(dir, "myapp")

	wantCommands := []recordedCommand{
		{dir: projectDir, name: "go", args: []string{"mod", "init", "myapp"}},
		{dir: projectDir, name: "go", args: []string{"mod", "tidy"}},
	}
	if len(runner.commands) != len(wantCommands) {
		t.Fatalf("commands = %+v, want %+v", runner.commands, wantCommands)
	}
	for i, want := range wantCommands {
		got := runner.commands[i]
		if got.dir != want.dir || got.name != want.name || strings.Join(got.args, " ") != strings.Join(want.args, " ") {
			t.Fatalf("command[%d] = %+v, want %+v", i, got, want)
		}
	}

	mainGo, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(mainGo)

	for _, want := range []string{
		`"github.com/angvp/tango"`,
		`"github.com/angvp/tango/db"`,
		`"github.com/angvp/tango/migration"`,
		`"myapp/migrations"`,
		`_ "modernc.org/sqlite"`,
		`sql.Open("sqlite", "app.db")`,
		`flag.Bool("check"`,
		`flag.Bool("tango-dump-models"`,
		`flag.Bool("tango-status"`,
		`flag.Bool("migrate"`,
		`flag.Bool("down"`,
		"InstalledApps: []tango.App{}",
	} {
		if !strings.Contains(mainSource, want) {
			t.Fatalf("main.go does not contain %q:\n%s", want, mainSource)
		}
	}

	migrationsGo, err := os.ReadFile(filepath.Join(projectDir, "migrations", "migrations.go"))
	if err != nil {
		t.Fatalf("read migrations/migrations.go: %v", err)
	}
	if !strings.Contains(string(migrationsGo), "var Migrations = []migration.Migration{}") {
		t.Fatalf("migrations.go does not declare empty Migrations slice:\n%s", migrationsGo)
	}
}

func TestNewProjectRequiresName(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	var stderr strings.Builder
	code := Run(context.Background(), []string{"newproject"}, dir, io.Discard, &stderr, runner)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("commands = %+v, want none run", runner.commands)
	}
}

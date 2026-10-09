package cli

import (
	"context"
	"errors"
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

func TestNewProjectCreatesRunnableSQLiteWiredProjectWithAdmin(t *testing.T) {
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
		`"github.com/angvp/tango/admin"`,
		`"github.com/angvp/tango/db"`,
		`"myapp/migrations"`,
		`"myapp/project"`,
		`_ "modernc.org/sqlite"`,
		`dsn, err := tango.LoadDBConfigFromEnv()`,
		`sql.Open(dsn.Driver, dsn.Source)`,
		`store := db.NewStore(sqlDB, dsn.Dialect)`,
		`tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations)`,
		`signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`,
		`defer stop()`,
		`tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)`,
		`config := project.Config(store)`,
		`admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)`,
	} {
		if !strings.Contains(mainSource, want) {
			t.Fatalf("main.go does not contain %q:\n%s", want, mainSource)
		}
	}
	// Application composition lives in the project package, not in main.go.
	for _, gone := range []string{"InstalledApps", "tango.MaxBodySize", "tango.LoadConfigFromEnv"} {
		if strings.Contains(mainSource, gone) {
			t.Fatalf("main.go still contains %q, which belongs in project/project.go:\n%s", gone, mainSource)
		}
	}
	projectGo, err := os.ReadFile(filepath.Join(projectDir, "project", "project.go"))
	if err != nil {
		t.Fatalf("read project/project.go: %v", err)
	}
	projectSource := string(projectGo)
	for _, want := range []string{
		`package project`,
		`func Config(store *db.Store) tango.Config {`,
		`config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())`,
		`admin.New(store)`,
		`tango.MaxBodySize(1 << 20)`,
		`config.MiddlewareScope = tango.MiddlewareScopeAll`,
		`tango.RequestID()`,
		`tango.Recoverer()`,
		`tango.AccessLogger()`,
	} {
		if !strings.Contains(projectSource, want) {
			t.Fatalf("project/project.go does not contain %q:\n%s", want, projectSource)
		}
	}
	// The shared package composes the application; it never loads the
	// .env file or opens a database (each process does that itself).
	for _, banned := range []string{"LoadEnvFile", "LoadDBConfigFromEnv", "sql.Open"} {
		if strings.Contains(projectSource, banned) {
			t.Fatalf("project/project.go calls %s; main.go and shell/main.go own that:\n%s", banned, projectSource)
		}
	}
	if strings.Contains(projectSource, `":8000"`) {
		t.Fatalf("project.go hardcodes :8000 instead of loading the address from the environment:\n%s", projectSource)
	}
	// The upload comment, read as one sentence across its wrapped lines.
	const uploadComment = "If this application later needs large uploads, remove the global body-limit middleware and apply `MaxBodySize` only to the route groups or routes that should remain limited."
	if !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(projectSource, "//", "")), " "), uploadComment) {
		t.Fatalf("project.go does not carry the upload comment %q:\n%s", uploadComment, projectSource)
	}

	// The generated file is the first code a reader sees, so its imports
	// form sorted groups, the way goimports leaves them.
	wantImports := "import (\n\t\"context\"\n\t\"database/sql\"\n\t\"fmt\"\n\t\"net\"\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n\n" +
		"\t\"github.com/angvp/tango\"\n\t\"github.com/angvp/tango/admin\"\n\t\"github.com/angvp/tango/db\"\n\n" +
		"\t\"myapp/migrations\"\n\t\"myapp/project\"\n\n\t_ \"modernc.org/sqlite\"\n)\n"
	if !strings.Contains(mainSource, wantImports) {
		t.Fatalf("main.go imports are not %q:\n%s", wantImports, mainSource)
	}

	migrationsGo, err := os.ReadFile(filepath.Join(projectDir, "migrations", "migrations.go"))
	if err != nil {
		t.Fatalf("read migrations/migrations.go: %v", err)
	}
	if !strings.Contains(string(migrationsGo), "var Migrations = []migration.Migration{}") {
		t.Fatalf("migrations.go does not declare empty Migrations slice:\n%s", migrationsGo)
	}

	gitignore, err := os.ReadFile(filepath.Join(projectDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignore), ".env") {
		t.Fatalf(".gitignore does not ignore .env:\n%s", gitignore)
	}
	// The first-run order: models become migrations, migrations are
	// applied, then the admin account can be created.
	out := stdout.String()
	last := -1
	for _, step := range []string{"tango makemigrations", "tango migrate", "tango admin create <username>"} {
		at := strings.Index(out, step)
		if at <= last {
			t.Fatalf("stdout does not list %q after the previous step:\n%s", step, out)
		}
		last = at
	}
}

func TestNewProjectNoAdminSkipsCredentials(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"newproject", "--no-admin", "myapp"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	projectDir := filepath.Join(dir, "myapp")
	mainGo, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	projectGo, err := os.ReadFile(filepath.Join(projectDir, "project", "project.go"))
	if err != nil {
		t.Fatalf("read project/project.go: %v", err)
	}
	if strings.Contains(string(mainGo), "admin.") || strings.Contains(string(projectGo), "admin.") {
		t.Fatalf("generated code contains admin wiring with --no-admin:\n%s\n%s", mainGo, projectGo)
	}
	// The shared Config takes the store even without the admin app, so the
	// shell can call it the same way in every project.
	if !strings.Contains(string(projectGo), "func Config(store *db.Store) tango.Config {") ||
		!strings.Contains(string(mainGo), "config := project.Config(store)") {
		t.Fatalf("--no-admin project does not share Config(store):\n%s\n%s", mainGo, projectGo)
	}
	if !strings.Contains(string(mainGo), "tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)") {
		t.Fatalf("main.go does not serve with the dialect from TANGO_DB_DSN:\n%s", mainGo)
	}
	wantImports := "import (\n\t\"context\"\n\t\"database/sql\"\n\t\"fmt\"\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n\n\t\"github.com/angvp/tango\"\n\t\"github.com/angvp/tango/db\"\n\n\t\"myapp/migrations\"\n\t\"myapp/project\"\n\n\t_ \"modernc.org/sqlite\"\n)\n"
	if !strings.Contains(string(mainGo), wantImports) {
		t.Fatalf("main.go imports are not %q:\n%s", wantImports, mainGo)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".env")); !os.IsNotExist(err) {
		t.Fatalf(".env stat = %v, want not exist", err)
	}
}

func TestNewProjectPostgresDialect(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"newproject", "--dialect=postgres", "myapp"}, dir, &stdout, &stderr, runner)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}

	mainGo, err := os.ReadFile(filepath.Join(dir, "myapp", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(mainGo)
	for _, want := range []string{
		`_ "github.com/jackc/pgx/v5/stdlib"`,
		`os.Setenv("TANGO_DB_DSN", "postgres://postgres:postgres@localhost:5432/myapp")`,
		`dsn, err := tango.LoadDBConfigFromEnv()`,
		`sql.Open(dsn.Driver, dsn.Source)`,
		`tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations)`,
		`tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("main.go does not contain %q:\n%s", want, source)
		}
	}
}

func TestNewProjectRejectsUnknownDialect(t *testing.T) {
	dir := t.TempDir()
	runner := &multiRecordingRunner{}

	var stderr strings.Builder
	code := Run(context.Background(), []string{"newproject", "--dialect=oracle", "myapp"}, dir, io.Discard, &stderr, runner)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unsupported dialect") {
		t.Fatalf("stderr = %q, want unsupported dialect", stderr.String())
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

func TestNewProjectRejectsUnknownFlag(t *testing.T) {
	dir := t.TempDir()
	var stderr strings.Builder
	code := Run(context.Background(), []string{"newproject", "--bogus", "myapp"}, dir, io.Discard, &stderr, &multiRecordingRunner{})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2, stderr: %s", code, stderr.String())
	}
}

// TestNewProjectFailsWhenProjectFileIsBlocked table-drives the exit-1
// filesystem-obstruction paths of newproject: something newproject expects
// to create or write into is instead blocked by a pre-existing file or
// directory of the wrong kind.
func TestNewProjectFailsWhenProjectFileIsBlocked(t *testing.T) {
	tests := []struct {
		name  string
		skip  func(t *testing.T) bool
		setup func(t *testing.T, dir string)
	}{
		{
			name: "project dir is blocked by a file",
			setup: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "myapp"), []byte("not a directory"), 0o644); err != nil {
					t.Fatalf("seed blocking file: %v", err)
				}
			},
		},
		{
			name: "main.go cannot be written",
			skip: func(t *testing.T) bool { return os.Geteuid() == 0 },
			setup: func(t *testing.T, dir string) {
				projectDir := filepath.Join(dir, "myapp")
				if err := os.MkdirAll(projectDir, 0o555); err != nil {
					t.Fatalf("mkdir read-only project dir: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(projectDir, 0o755) })
			},
		},
		{
			name: "migrations path is blocked by a file",
			setup: func(t *testing.T, dir string) {
				projectDir := filepath.Join(dir, "myapp")
				if err := os.MkdirAll(projectDir, 0o755); err != nil {
					t.Fatalf("mkdir project dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(projectDir, "migrations"), []byte("not a directory"), 0o644); err != nil {
					t.Fatalf("seed blocking file: %v", err)
				}
			},
		},
		{
			name: "migrations.go is blocked by a directory",
			setup: func(t *testing.T, dir string) {
				migrationsDir := filepath.Join(dir, "myapp", "migrations")
				if err := os.MkdirAll(filepath.Join(migrationsDir, "migrations.go"), 0o755); err != nil {
					t.Fatalf("seed blocking directory: %v", err)
				}
			},
		},
		{
			name: ".gitignore is blocked by a directory",
			setup: func(t *testing.T, dir string) {
				projectDir := filepath.Join(dir, "myapp")
				if err := os.MkdirAll(filepath.Join(projectDir, ".gitignore"), 0o755); err != nil {
					t.Fatalf("seed blocking directory: %v", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil && tt.skip(t) {
				t.Skip("root ignores directory permission bits")
			}
			dir := t.TempDir()
			tt.setup(t, dir)

			var stderr strings.Builder
			code := Run(context.Background(), []string{"newproject", "myapp"}, dir, io.Discard, &stderr, &multiRecordingRunner{})
			if code != 1 {
				t.Fatalf("exit code = %d, want 1, stderr: %s", code, stderr.String())
			}
		})
	}
}

// TestNewProjectReportsCommandRunnerFailure table-drives newproject
// propagating the underlying error from each external command it runs (go
// mod init, then go mod tidy).
func TestNewProjectReportsCommandRunnerFailure(t *testing.T) {
	tests := []struct {
		name        string
		runner      Runner
		wantErr     string
		checkRunner func(t *testing.T, runner Runner)
	}{
		{
			name:    "go mod init failure",
			runner:  &multiRecordingRunner{err: errors.New("go mod init failed")},
			wantErr: "go mod init failed",
		},
		{
			name:    "go mod tidy failure",
			runner:  &failOnCallRunner{failOnCall: 2, err: errors.New("go mod tidy failed")},
			wantErr: "go mod tidy failed",
			checkRunner: func(t *testing.T, runner Runner) {
				got := runner.(*failOnCallRunner)
				if got.calls != 2 {
					t.Fatalf("runner.calls = %d, want exactly 2 (init succeeds, tidy fails)", got.calls)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			var stderr strings.Builder
			code := Run(context.Background(), []string{"newproject", "myapp"}, dir, io.Discard, &stderr, tt.runner)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1, stderr: %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tt.wantErr)
			}
			if tt.checkRunner != nil {
				tt.checkRunner(t, tt.runner)
			}
		})
	}
}

// TestWriteFormattedFileRejectsInvalidGoSource covers writeFormattedFile's
// own format.Source error path directly: every real caller only ever
// passes generated, always-valid Go source, so this branch is otherwise
// unreachable through the public newproject/newapp commands.
func TestWriteFormattedFileRejectsInvalidGoSource(t *testing.T) {
	dir := t.TempDir()
	err := writeFormattedFile(filepath.Join(dir, "broken.go"), "func broken(")
	if err == nil {
		t.Fatal("writeFormattedFile error = nil, want a syntax error for invalid Go source")
	}
}

type failOnCallRunner struct {
	calls      int
	failOnCall int
	err        error
}

func (r *failOnCallRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	r.calls++
	if r.calls == r.failOnCall {
		return r.err
	}
	return nil
}

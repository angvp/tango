package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shellSession is the shared walkthrough in testdata/shell_session.txt: what
// is typed, and what the shell prints for it.
type shellSession struct {
	input string // every typed line, one per line
	want  string // everything printed to stdout
}

func loadShellSession(t *testing.T) shellSession {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "shell_session.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var typed, printed []string
	for _, line := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "> "):
			typed = append(typed, strings.TrimPrefix(line, "> "))
		default:
			printed = append(printed, line)
		}
	}
	return shellSession{input: strings.Join(typed, "\n") + "\n", want: strings.Join(printed, "\n") + "\n"}
}

const postsApp = `package posts

import "github.com/angvp/tango"

type Post struct {
	ID    int64 ` + "`tango:\"pk\"`" + `
	Title string
	Body  string
}

var App = tango.NewApp("posts", func(r *tango.Registry) error {
	return r.Models().Register(Post{})
})
`

// scaffoldWithPosts creates a project (newproject flags first) with a posts
// app wired into project.Config, against this checkout.
func scaffoldWithPosts(t *testing.T, name string, flags ...string) (project, dir string) {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	var stderr strings.Builder
	args := append(append([]string{"newproject"}, flags...), name)
	if code := Run(context.Background(), args, dir, os.Stderr, &stderr, localTangoRunner{repo: repo}); code != 0 {
		t.Fatalf("newproject exit code = %d: %s", code, stderr.String())
	}
	project = filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(project, "apps", "posts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "apps", "posts", "app.go"), []byte(postsApp), 0o644); err != nil {
		t.Fatal(err)
	}
	projectGo := filepath.Join(project, "project", "project.go")
	source, err := os.ReadFile(projectGo)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(source), "\t\tadmin.New(store),", "\t\tposts.App,\n\t\tadmin.New(store),", 1)
	edited = strings.Replace(edited, "import (\n", "import (\n\t\""+name+"/apps/posts\"\n", 1)
	if err := os.WriteFile(projectGo, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return project, dir
}

// migratedSQLiteProject is scaffoldWithPosts with its migrations applied to a
// file database. Its pristine copy is kept beside it, for restoreDatabase.
func migratedSQLiteProject(t *testing.T, name string) (project, database string) {
	t.Helper()
	project, dir := scaffoldWithPosts(t, name)
	database = filepath.Join(dir, "app.db")
	t.Setenv("TANGO_DB_DSN", "sqlite://"+database)
	for _, args := range [][]string{{"makemigrations"}, {"migrate"}} {
		var out bytes.Buffer
		if code := Run(context.Background(), args, project, &out, &out, ExecRunner{}); code != 0 {
			t.Fatalf("tango %v exit code = %d:\n%s", args, code, out.String())
		}
	}
	content, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database+".pristine", content, 0o644); err != nil {
		t.Fatal(err)
	}
	return project, database
}

// restoreDatabase puts the freshly migrated, empty database back.
func restoreDatabase(t *testing.T, database string) {
	t.Helper()
	content, err := os.ReadFile(database + ".pristine")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShellAgainstAScaffoldedProject(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, builds and runs a project")
	}
	project, database := migratedSQLiteProject(t, "blog")
	session := loadShellSession(t)

	t.Run("-c runs the whole session", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{"shell", "-c", session.input}, project, &out, &errOut, ExecRunner{})
		if code != 0 || out.String() != session.want {
			t.Fatalf("code=%d\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", code, out.String(), session.want, errOut.String())
		}
		if !strings.Contains(errOut.String(), "database: sqlite: ") {
			t.Errorf("stderr = %q, want the database target line", errOut.String())
		}
	})

	// Start again from the migrated, empty database for the piped run.
	t.Run("piped standard input runs the same session", func(t *testing.T) {
		restoreDatabase(t, database)
		binary := buildShell(t, project)
		cmd := exec.Command(binary)
		cmd.Dir = project
		cmd.Stdin = strings.NewReader(session.input)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil || out.String() != session.want {
			t.Fatalf("err=%v\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", err, out.String(), session.want, errOut.String())
		}
	})

	t.Run("--readonly refuses writes and still reads", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{"shell", "--readonly", "-c", "Count(\"posts.Post\")\nCreate(\"posts.Post\", map[string]any{\"Title\": \"x\"})\nCount(\"posts.Post\")"}, project, &out, &errOut, ExecRunner{})
		if code != 1 || out.String() != "1\n" || !strings.Contains(errOut.String(), "read-only") {
			t.Fatalf("code=%d stdout=%q stderr=%q, want one read, then a refusal and exit 1", code, out.String(), errOut.String())
		}
	})

	t.Run("the first error stops the run with a non-zero exit", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{"shell", "-c", "Count(\"posts.Nope\")\n1"}, project, &out, &errOut, ExecRunner{})
		if code != 1 || out.String() != "" || !strings.Contains(errOut.String(), "error: unknown model") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	})

	t.Run("a recovered panic is reported and exits non-zero", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{"shell", "-c", "for v := range func(yield func(int) bool) { yield(1) } { _ = v }"}, project, &out, &errOut, ExecRunner{})
		if code != 1 || !strings.Contains(errOut.String(), "panic: ") {
			t.Fatalf("code=%d stderr=%q, want a panic: line and exit 1", code, errOut.String())
		}
	})

	t.Run("a command line the shell does not understand exits 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{"shell", "--nope"}, project, &out, &errOut, ExecRunner{})
		if code != 2 || !strings.Contains(errOut.String(), "usage: tango shell") {
			t.Fatalf("code=%d stderr=%q, want the shell's own exit code 2 and usage", code, errOut.String())
		}
	})

	t.Run("--help prints the usage and exits 0", func(t *testing.T) {
		var out bytes.Buffer
		if code := Run(context.Background(), []string{"shell", "--help"}, project, &out, os.Stderr, ExecRunner{}); code != 0 || !strings.Contains(out.String(), "usage: tango shell") {
			t.Fatalf("code=%d stdout=%q", code, out.String())
		}
	})

	t.Run("a project without shell/main.go says which file is missing", func(t *testing.T) {
		bare := t.TempDir()
		var errOut bytes.Buffer
		code := Run(context.Background(), []string{"shell"}, bare, os.Stdout, &errOut, ExecRunner{})
		if code != 1 || !strings.Contains(errOut.String(), "shell/main.go") {
			t.Fatalf("code=%d stderr=%q", code, errOut.String())
		}
	})

	t.Run("only the shell links the interpreter", func(t *testing.T) {
		for _, tt := range []struct {
			pkg     string
			wantDep bool
		}{{".", false}, {"./shell", true}} {
			cmd := exec.Command("go", "list", "-deps", tt.pkg)
			cmd.Dir = project
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list -deps %s: %v\n%s", tt.pkg, err, out)
			}
			if has := strings.Contains(string(out), "github.com/traefik/yaegi/interp"); has != tt.wantDep {
				t.Errorf("go list -deps %s: links the interpreter = %v, want %v", tt.pkg, has, tt.wantDep)
			}
		}
	})
}

// TestShellHelpersRegisteredInTheProjectsShellMain adds the helpers snippet
// the shell guide shows to a scaffolded shell/main.go and calls them.
func TestShellHelpersRegisteredInTheProjectsShellMain(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, builds and runs a project")
	}
	project, _ := scaffoldWithPosts(t, "blog")
	snippet, err := os.ReadFile(filepath.Join("testdata", "shell_helpers_snippet.txt"))
	if err != nil {
		t.Fatal(err)
	}
	mainGo := filepath.Join(project, "shell", "main.go")
	source, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(source), "shell.Options{\n", "shell.Options{\n"+string(snippet), 1)
	edited = strings.Replace(edited, "import (\n", "import (\n\t\"errors\"\n", 1)
	if edited == string(source) {
		t.Fatal("the scaffolded shell/main.go has no shell.Options literal to add helpers to")
	}
	if err := os.WriteFile(mainGo, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGO_DB_DSN", "sqlite://"+filepath.Join(t.TempDir(), "app.db"))

	run := func(args ...string) (code int, out, errOut string) {
		var o, e bytes.Buffer
		code = Run(context.Background(), append([]string{"shell"}, args...), project, &o, &e, ExecRunner{})
		return code, o.String(), e.String()
	}
	for _, pair := range loadTranscript(t, "shell_helpers_session.txt") {
		code, out, errOut := run("-c", pair.input)
		var shown []string
		for _, line := range strings.Split(strings.TrimRight(out+errOut, "\n"), "\n") {
			if !strings.HasPrefix(line, "database: ") {
				shown = append(shown, line)
			}
		}
		if got := strings.Join(shown, "\n"); got != pair.output {
			t.Errorf("%s printed %q, want %q", pair.input, got, pair.output)
		}
		if failed := strings.HasPrefix(pair.output, "error: "); failed != (code != 0) {
			t.Errorf("%s: exit code %d, but the expected output is an error: %v", pair.input, code, failed)
		}
	}
	if code, out, _ := run("-c", "help()"); code != 0 || !strings.Contains(out, "project.Fail\n  project.Greeting\n") {
		t.Fatalf("help: code=%d out=%q, want the helper names", code, out)
	}
}

// transcriptPair is one typed line and everything the shell printed for it.
type transcriptPair struct{ input, output string }

// loadTranscript reads a testdata session file ("> " lines are typed, the
// lines after one are printed) as typed/printed pairs.
func loadTranscript(t *testing.T, name string) []transcriptPair {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var pairs []transcriptPair
	for _, line := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "> "):
			pairs = append(pairs, transcriptPair{input: strings.TrimPrefix(line, "> ")})
		case len(pairs) > 0:
			last := &pairs[len(pairs)-1]
			if last.output != "" {
				last.output += "\n"
			}
			last.output += line
		}
	}
	return pairs
}

// buildShell builds the project's shell program and returns the binary.
func buildShell(t *testing.T, project string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "shell-bin")
	build := exec.Command("go", "build", "-o", binary, "./shell")
	build.Dir = project
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./shell: %v\n%s", err, out)
	}
	return binary
}

// The database line names the target and never a credential.
func TestShellDatabaseLabelNeverShowsCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, builds and runs a project")
	}
	sqliteProject, _ := migratedSQLiteProject(t, "blog")
	sqliteShell := buildShell(t, sqliteProject)
	postgresProject, _ := scaffoldWithPosts(t, "shop", "--dialect=postgres")
	postgresShell := buildShell(t, postgresProject)
	tests := []struct {
		name, dsn, want string
		secrets         []string
	}{
		{"sqlite relative", "sqlite://app.db", "database: sqlite: app.db\n", nil},
		{"sqlite absolute", "sqlite:///var/data/app.db", "database: sqlite: /var/data/app.db\n", nil},
		{"sqlite memory", "sqlite://:memory:", "database: sqlite: :memory:\n", nil},
		{"postgres with userinfo", "postgres://admin:s3cret@db.example.com:5432/shop?sslmode=disable", "database: postgres: db.example.com:5432/shop\n", []string{"admin", "s3cret"}},
		{"postgresql scheme", "postgresql://admin:s3cret@db.example.com/shop", "database: postgres: db.example.com/shop\n", []string{"admin", "s3cret"}},
		{"password in the query", "postgres://db.example.com/shop?password=qwerty123&sslpassword=zz9plural", "database: postgres: db.example.com/shop\n", []string{"qwerty123", "zz9plural"}},
		{"escaped password", "postgres://admin:p%40ss%2Fword@db.example.com/shop", "database: postgres: db.example.com/shop\n", []string{"admin", "p%40ss", "p@ss"}},
		{"user without password", "postgres://admin@db.example.com/shop", "database: postgres: db.example.com/shop\n", []string{"admin"}},
		{"ipv6 host", "postgres://admin:s3cret@[::1]:5432/shop", "database: postgres: [::1]:5432/shop\n", []string{"admin", "s3cret"}},
		{"unescaped slash in the password", "postgres://admin:s3cret/pa55@db.example.com/shop", "database: postgres: (address hidden)\n", []string{"admin", "s3cret", "pa55"}},
		{"unescaped at sign in the password", "postgres://admin:s3@cret@db.example.com/shop", "database: postgres: (address hidden)\n", []string{"admin", "s3", "cret"}},
		{"at sign after a question mark", "postgres://admin:12345?abc@db.example.com/shop", "database: postgres: (address hidden)\n", []string{"admin", "12345", "abc"}},
		{"empty password then a question mark", "postgres://admin:?x@db.example.com/shop", "database: postgres: (address hidden)\n", []string{"admin", "x@"}},
		{"at sign after a hash", "postgres://admin:#x@db.example.com/shop", "database: postgres: (address hidden)\n", []string{"admin", "#x"}},
		{"malformed port", "postgres://admin:s3cret@db.example.com:port/shop", "database: postgres: (address hidden)\n", []string{"admin", "s3cret"}},
		{"no host", "postgres://admin:s3cret@/shop", "database: postgres: (address hidden)\n", []string{"admin", "s3cret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary, project := postgresShell, postgresProject
			if strings.HasPrefix(tt.dsn, "sqlite") {
				binary, project = sqliteShell, sqliteProject
			}
			cmd := exec.Command(binary, "-c", "1")
			cmd.Dir = project
			cmd.Env = append(os.Environ(), "TANGO_DB_DSN="+tt.dsn)
			var out, errOut bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errOut
			if err := cmd.Run(); err != nil {
				t.Fatalf("shell failed: %v\nstderr: %s", err, errOut.String())
			}
			if !strings.HasPrefix(errOut.String(), tt.want) {
				t.Errorf("stderr = %q, want it to start with %q", errOut.String(), tt.want)
			}
			for _, secret := range tt.secrets {
				if strings.Contains(out.String()+errOut.String(), secret) {
					t.Errorf("output leaks %q:\nstdout=%q\nstderr=%q", secret, out.String(), errOut.String())
				}
			}
		})
	}

	t.Run("a DSN the driver rejects does not leak it in the error", func(t *testing.T) {
		cmd := exec.Command(postgresShell, "-c", "Count(\"posts.Post\")")
		cmd.Dir = postgresProject
		cmd.Env = append(os.Environ(), "TANGO_DB_DSN=postgres://admin:s3cret@db.invalid:5432/shop?connect_timeout=1")
		out, _ := cmd.CombinedOutput()
		if strings.Contains(string(out), "s3cret") {
			t.Errorf("an error path leaks the password:\n%s", out)
		}
	})
}

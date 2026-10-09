package cli

import (
	"context"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type projectDialect struct {
	DriverImport string
	// DefaultDSN is the TANGO_DB_DSN the generated main.go falls back to
	// when the variable is unset, or empty to keep tango.LoadDBConfigFromEnv's
	// own default (sqlite://app.db).
	DefaultDSN string
}

var projectDialects = map[string]projectDialect{
	"sqlite": {
		DriverImport: `modernc.org/sqlite`,
	},
	"postgres": {
		DriverImport: `github.com/jackc/pgx/v5/stdlib`,
		DefaultDSN:   "postgres://postgres:postgres@localhost:5432/THIS_MODULE",
	},
}

func newProject(ctx context.Context, runner Runner, dir string, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("newproject", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dialectName := flags.String("dialect", "sqlite", "database dialect: sqlite or postgres")
	noAdmin := flags.Bool("no-admin", false, "generate project without the admin app")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() < 1 {
		fmt.Fprintln(stderr, "tango newproject: a project name is required")
		return 2
	}
	name := flags.Arg(0)

	dialect, ok := projectDialects[*dialectName]
	if !ok {
		fmt.Fprintf(stderr, "tango newproject: unsupported dialect %q (want sqlite or postgres)\n", *dialectName)
		return 2
	}
	dialect.DefaultDSN = strings.ReplaceAll(dialect.DefaultDSN, "THIS_MODULE", name)

	projectDir := filepath.Join(dir, name)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	if err := runner.Run(ctx, projectDir, "go", []string{"mod", "init", name}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	projectPkg := filepath.Join(projectDir, "project")
	if err := os.MkdirAll(projectPkg, 0o755); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}
	if err := writeFormattedFile(filepath.Join(projectPkg, "project.go"), renderProjectGo(!*noAdmin)); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	shellDir := filepath.Join(projectDir, "shell")
	if err := os.MkdirAll(shellDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}
	if err := writeFormattedFile(filepath.Join(shellDir, "main.go"), renderShellMain(name, dialect)); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	mainGo := renderNewProjectMain(name, dialect, !*noAdmin)
	if err := writeFormattedFile(filepath.Join(projectDir, "main.go"), mainGo); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(filepath.Join(projectDir, "migrations"), 0o755); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}
	if err := writeFormattedFile(filepath.Join(projectDir, "migrations", "migrations.go"), newProjectMigrationsGo); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	if err := os.WriteFile(filepath.Join(projectDir, ".gitignore"), []byte(".env\napp.db\n"), 0o644); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	if err := runner.Run(ctx, projectDir, "go", []string{"mod", "tidy"}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "created %s\n", name)
	if !*noAdmin {
		fmt.Fprintln(stdout, "next: run `tango makemigrations`, then `tango migrate`, then `tango admin create <username>` to create your first admin account")
	}
	return 0
}

func writeFormattedFile(path string, source string) error {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return err
	}
	return os.WriteFile(path, formatted, 0o644)
}

const newProjectMigrationsGo = `package migrations

import "github.com/angvp/tango/migration"

var Migrations = []migration.Migration{}
`

// renderImports renders an import block with one sorted group per entry
// of groups, the way goimports leaves them, followed by blankImport (the
// database driver) imported for its side effects in a group of its own.
func renderImports(groups [][]string, blankImport string) string {
	var b strings.Builder
	b.WriteString("import (\n")
	for _, group := range groups {
		for _, path := range slices.Sorted(slices.Values(group)) {
			b.WriteString("\t" + strconv.Quote(path) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("\t_ " + strconv.Quote(blankImport) + "\n)")
	return b.String()
}

// adminURLFunc is the helper the generated main.go uses to print where the
// admin can be opened.
const adminURLFunc = `
// adminURL is where the admin can be opened from this machine for the listen
// address addr, or "" when addr is not a host:port.
func adminURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/admin/"
}
`

// renderProjectGo renders project/project.go: the application's composition
// (installed apps, middleware, ordinary configuration), shared by the server
// in main.go and the shell. It neither loads the .env file nor opens a
// database, so each process keeps control of its own lifecycle.
func renderProjectGo(includeAdmin bool) string {
	tangoImports := []string{"github.com/angvp/tango", "github.com/angvp/tango/db"}
	installedApps := "config.InstalledApps = []tango.App{}"
	if includeAdmin {
		tangoImports = append(tangoImports, "github.com/angvp/tango/admin")
		installedApps = `config.InstalledApps = []tango.App{
		admin.New(store),
	}`
	}
	var imports strings.Builder
	imports.WriteString("import (\n")
	for _, path := range slices.Sorted(slices.Values(tangoImports)) {
		imports.WriteString("\t" + strconv.Quote(path) + "\n")
	}
	imports.WriteString(")")

	return fmt.Sprintf(`package project

%s

// Config composes the application: its installed apps, middleware and
// ordinary configuration. The server (main.go) and the shell (shell/main.go)
// both call it, so they always register the same apps and models. Add each
// new app to InstalledApps here.
//
// Config does not load the .env file or open a database: each process does
// that itself and passes the store in.
func Config(store *db.Store) tango.Config {
	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	%s
	config.Middleware = []tango.Middleware{
		tango.RequestID(),
		tango.Recoverer(),
		tango.AccessLogger(),
		// Request bodies are capped at 1 MiB. If this application later needs
		// large uploads, remove the global body-limit middleware and apply
		// `+"`MaxBodySize`"+` only to the route groups or routes that should remain
		// limited.
		tango.MaxBodySize(1 << 20),
	}
	// Global middleware also wraps requests no route matches, so 404s and
	// 405s are logged and counted too.
	config.MiddlewareScope = tango.MiddlewareScopeAll
	return config
}
`, imports.String(), installedApps)
}

func renderNewProjectMain(module string, dialect projectDialect, includeAdmin bool) string {
	stdImports := []string{"context", "database/sql", "fmt", "os", "os/signal", "syscall"}
	adminURLBlock, adminURLHelper := "", ""
	tangoImports := []string{"github.com/angvp/tango", "github.com/angvp/tango/db"}
	adminCLIBlock := ""
	if includeAdmin {
		stdImports = []string{"context", "database/sql", "fmt", "net", "os", "os/signal", "syscall"}
		tangoImports = append(tangoImports, "github.com/angvp/tango/admin")
		adminURLBlock = `
	if url := adminURL(config.Addr); url != "" {
		fmt.Println("admin:", url)
	}`
		adminURLHelper = adminURLFunc
		adminCLIBlock = `
	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}
`
	}

	defaultDSNBlock := ""
	if dialect.DefaultDSN != "" {
		defaultDSNBlock = fmt.Sprintf(`
	if os.Getenv("TANGO_DB_DSN") == "" {
		os.Setenv("TANGO_DB_DSN", %q)
	}
`, dialect.DefaultDSN)
	}

	imports := renderImports(
		[][]string{stdImports, tangoImports, {module + "/migrations", module + "/project"}},
		dialect.DriverImport,
	)

	return fmt.Sprintf(`package main

%s

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := tango.LoadEnvFile(".env"); err != nil {
		return err
	}
%s
	dsn, err := tango.LoadDBConfigFromEnv()
	if err != nil {
		return err
	}

	sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, dsn.Dialect)
	// The installed apps, middleware and other configuration live in
	// project/project.go, shared with the shell.
	config := project.Config(store)
%s
	handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations)
	if handled || err != nil {
		return err
	}

	// Ctrl-C and SIGTERM (what hosting platforms send on a deploy) stop the
	// server gracefully: in-flight requests finish before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("listening on", config.Addr)%s
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
}
%s`, imports, defaultDSNBlock, adminCLIBlock, adminURLBlock, adminURLHelper)
}

// renderShellMain renders shell/main.go: the project's own `tango shell`
// program. It lives in the project's module because only there are the
// project's apps and models linked, and it shares project.Config with the
// server so both register the same apps.
func renderShellMain(module string, dialect projectDialect) string {
	defaultDSNBlock := ""
	if dialect.DefaultDSN != "" {
		defaultDSNBlock = fmt.Sprintf(`
	if os.Getenv("TANGO_DB_DSN") == "" {
		os.Setenv("TANGO_DB_DSN", %q)
	}
`, dialect.DefaultDSN)
	}
	imports := renderImports(
		[][]string{
			{"context", "database/sql", "fmt", "net/url", "os", "strings"},
			{"github.com/angvp/tango", "github.com/angvp/tango/db", "github.com/angvp/tango/shell"},
			{module + "/project"},
		},
		dialect.DriverImport,
	)
	return fmt.Sprintf(`// Command shell is this project's "tango shell": an interactive Go console
// with the project's apps, models and database loaded and nothing served.
// Run it with "tango shell". It executes local code with this project's
// database credentials: it is not a sandbox.
package main

%s

func main() {
	os.Exit(run())
}

func run() int {
	if err := tango.LoadEnvFile(".env"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
%s
	dsn, err := tango.LoadDBConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, dsn.Dialect)
	return shell.Run(context.Background(), project.Config(store), store, os.Args[1:], shell.Options{
		DatabaseLabel: databaseLabel(dsn),
	})
}
%s`, imports, defaultDSNBlock, databaseLabelFunc)
}

// databaseLabelFunc is the generated shell's databaseLabel. The label is
// shown at startup so a person can see which database they are about to
// touch; it must never carry a user name, password or anything else that
// could be a credential, so it names only the file or the host and database.
const databaseLabelFunc = `
// databaseLabel says which database the shell works on, without any part
// of the DSN that could be a credential: for SQLite the file, for PostgreSQL
// only the host and database name. Anything it cannot read with confidence
// is hidden rather than shown.
func databaseLabel(dsn db.DSN) string {
	if dsn.Dialect != db.Postgres {
		path, _, _ := strings.Cut(dsn.Source, "?")
		return "sqlite: " + strings.TrimPrefix(path, "file:")
	}
	const hidden = "postgres: (address hidden)"
	_, rest, ok := strings.Cut(dsn.Source, "://")
	if !ok {
		return hidden
	}
	// The address ends at the first "/", "?" or "#". A "@" after that means
	// the userinfo was not escaped, so what follows cannot be told apart
	// from a password.
	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = rest[:end]
	}
	if strings.Count(rest, "@") != strings.Count(authority, "@") || strings.Count(authority, "@") > 1 {
		return hidden
	}
	u, err := url.Parse(dsn.Source)
	if err != nil || u.Host == "" {
		return hidden
	}
	return "postgres: " + u.Host + "/" + strings.TrimPrefix(u.Path, "/")
}
`

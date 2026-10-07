package cli

import (
	"context"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
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
		fmt.Fprintln(stdout, "run `tango migrate` then `tango admin create <username>` to create your first admin account")
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

func renderNewProjectMain(module string, dialect projectDialect, includeAdmin bool) string {
	adminImport := ""
	adminConfig := "InstalledApps: []tango.App{},"
	contextImport := ""
	storeLine := ""
	adminCLIBlock := ""
	if includeAdmin {
		adminImport = "\n\t\"github.com/angvp/tango/admin\"\n\t\"github.com/angvp/tango/db\""
		contextImport = "\"context\"\n\t"
		storeLine = "store := db.NewStore(sqlDB, dialect)"
		adminConfig = `InstalledApps: []tango.App{
			admin.New(store),
		},`
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

	return fmt.Sprintf(`package main

import (
	%s"database/sql"
	"fmt"
	"os"

	"github.com/angvp/tango"%s

	"%s/migrations"

	_ "%s"
)

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
	dialect, driverName, dsn, err := tango.LoadDBConfigFromEnv()
	if err != nil {
		return err
	}

	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	%s
	config := tango.Config{
		%s
		Addr: ":8000",
	}
%s
	handled, err := tango.DispatchFlags(config, sqlDB, dialect, migrations.Migrations)
	if handled || err != nil {
		return err
	}

	fmt.Println("listening on", config.Addr)
	return tango.Serve(config, sqlDB, dialect)
}
`, contextImport, adminImport, module, dialect.DriverImport, defaultDSNBlock, storeLine, adminConfig, adminCLIBlock)
}

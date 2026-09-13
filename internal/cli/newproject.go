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
	Name         string
	DriverName   string
	DriverImport string
	DialectExpr  string
	DefaultDSN   string
	// OpenDSNExpr is the Go expression the generated main.go passes to
	// sql.Open — plain "dsn" for dialects with no extra per-connection
	// setup, or a wrapping call (e.g. db.SQLiteForeignKeysDSN(dsn)) for a
	// dialect that needs one.
	OpenDSNExpr string
}

var projectDialects = map[string]projectDialect{
	"sqlite": {
		Name:         "sqlite",
		DriverName:   "sqlite",
		DriverImport: `modernc.org/sqlite`,
		DialectExpr:  "db.SQLite",
		DefaultDSN:   "app.db",
		// Enables foreign key constraint enforcement on every connection
		// the driver opens — SQLite treats this as off by default and
		// per-connection, not a database-wide setting. See ADR 0010/0011.
		OpenDSNExpr: "db.SQLiteForeignKeysDSN(dsn)",
	},
	"postgres": {
		Name:         "postgres",
		DriverName:   "pgx",
		DriverImport: `github.com/jackc/pgx/v5/stdlib`,
		DialectExpr:  "db.Postgres",
		DefaultDSN:   "postgres://postgres:postgres@localhost:5432/THIS_MODULE",
		OpenDSNExpr:  "dsn",
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
		adminImport = "\n\t\"github.com/angvp/tango/admin\""
		contextImport = "\n\t\"context\""
		storeLine = "store := db.NewStore(sqlDB, " + dialect.DialectExpr + ")"
		adminConfig = `InstalledApps: []tango.App{
			admin.New(store),
		},`
		adminCLIBlock = `
	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}
`
	}

	return fmt.Sprintf(`package main

import (
	"database/sql"
	"fmt"
	"os"
	%s

	"github.com/angvp/tango"%s
	"github.com/angvp/tango/db"

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
	if os.Getenv("TANGO_DB_DIALECT") == "" {
		os.Setenv("TANGO_DB_DIALECT", %q)
	}

	dsn := tango.LoadDBDSNFromEnv()
	if dsn == "" {
		dsn = %q
	}

	sqlDB, err := sql.Open(%q, %s)
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
	handled, err := tango.DispatchFlags(config, sqlDB, %s, migrations.Migrations)
	if handled || err != nil {
		return err
	}

	fmt.Println("listening on", config.Addr)
	return tango.Serve(config, sqlDB, %s)
}
`, contextImport, adminImport, module, dialect.DriverImport, dialect.Name, dialect.DefaultDSN, dialect.DriverName, dialect.OpenDSNExpr, storeLine, adminConfig, adminCLIBlock, dialect.DialectExpr, dialect.DialectExpr)
}

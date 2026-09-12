package cli

import (
	"context"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// newProject scaffolds a runnable tanGO project pre-wired for SQLite: a new
// Go module plus a main.go implementing the CLI flag-dispatch convention
// (-check, -tango-dump-models, -migrate[-down]) with no app registered yet.
// See Milestone 8.2: this exists so the developer-documentation tutorial
// (Milestone 9) can bootstrap real, tested scaffolding instead of hand-typed
// boilerplate.
func newProject(ctx context.Context, runner Runner, dir string, name string, stdout io.Writer, stderr io.Writer) int {
	if name == "" {
		fmt.Fprintln(stderr, "tango newproject: a project name is required")
		return 2
	}

	projectDir := filepath.Join(dir, name)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	if err := runner.Run(ctx, projectDir, "go", []string{"mod", "init", name}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	mainGo := strings.Replace(newProjectMainGo, "THIS_MODULE", name, 1)
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

	if err := runner.Run(ctx, projectDir, "go", []string{"mod", "tidy"}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "tango newproject: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "created %s\n", name)
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

const newProjectMainGo = `package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	"THIS_MODULE/migrations"

	_ "modernc.org/sqlite"
)

func main() {
	os.Exit(run())
}

func run() int {
	check := flag.Bool("check", false, "validate app registration and exit")
	dumpModels := flag.Bool("tango-dump-models", false, "print registered models as JSON and exit")
	status := flag.Bool("tango-status", false, "print project status as JSON and exit")
	migrateFlag := flag.Bool("migrate", false, "apply pending migrations and exit")
	down := flag.Bool("down", false, "roll back the last applied migration (with -migrate)")
	flag.Parse()

	config := tango.Config{
		InstalledApps: []tango.App{},
		Addr:          ":8000",
	}

	if *dumpModels {
		models, err := tango.DumpModels(config)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := json.NewEncoder(os.Stdout).Encode(models); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if *check {
		if err := tango.Check(config); err != nil {
			fmt.Fprintln(os.Stderr, "check failed:", err)
			return 1
		}
		fmt.Println("check passed")
		return 0
	}

	sqlDB, err := sql.Open("sqlite", "app.db")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sqlDB.Close()

	ctx := context.Background()

	if *status {
		result := tango.Status(ctx, config, sqlDB, db.SQLite, migrations.Migrations)
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if *migrateFlag {
		if *down {
			if err := migration.RollbackLast(ctx, sqlDB, db.SQLite, migrations.Migrations); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fmt.Println("rolled back last migration")
			return 0
		}
		if err := migration.ApplyPending(ctx, sqlDB, db.SQLite, migrations.Migrations); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("migrations applied")
		return 0
	}

	registry, err := tango.BuildRegistry(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := registry.RunRegistration(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	registry.SetStore(db.NewStore(sqlDB, db.SQLite))

	handler, err := registry.Routes().Handler()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Println("listening on", config.Addr)
	if err := http.ListenAndServe(config.Addr, handler); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
`

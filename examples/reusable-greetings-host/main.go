// reusable-greetings-host is the Host project for this example pair: it
// imports the "reusable-greetings" module by its real module path (see the
// require/replace in go.mod, standing in for a real published version) and
// installs its App into InstalledApps alongside a local app it scaffolded
// itself. No internals of the reusable app are copied here.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	"reusable-greetings/greetings"
	greetingsmigrations "reusable-greetings/migrations"

	"reusable-greetings-host/apps/echo"
	"reusable-greetings-host/migrations"

	_ "modernc.org/sqlite"
)

func main() {
	os.Exit(run())
}

func run() int {
	sqlDB, err := sql.Open("sqlite", "app.db")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, db.SQLite)

	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	check := flag.Bool("check", false, "validate app registration and exit")
	dumpModels := flag.Bool("tango-dump-models", false, "print registered models as JSON and exit")
	status := flag.Bool("tango-status", false, "print project status as JSON and exit")
	migrateFlag := flag.Bool("migrate", false, "apply pending migrations and exit")
	down := flag.Bool("down", false, "roll back the last applied migration (with -migrate)")
	flag.Parse()

	config := tango.Config{
		InstalledApps: []tango.App{
			echo.App{},
			greetings.New(store),
			admin.New(store),
		},
		Addr: ":8000",
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

	ctx := context.Background()

	// Contributed migrations (greetingsmigrations.Migrations, shipped inside
	// the reusable app's own repo) are concatenated with this host's own
	// migrations, in InstalledApps order (greetings.New before this host's
	// own local migrations), before anything is applied or reported — see
	// docs/guides/reusable-apps.md.
	var allMigrations []migration.Migration
	allMigrations = append(allMigrations, greetingsmigrations.Migrations...)
	allMigrations = append(allMigrations, migrations.Migrations...)

	if *status {
		result := tango.Status(ctx, config, sqlDB, db.SQLite, allMigrations)
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if *migrateFlag {
		if *down {
			if err := migration.RollbackLast(ctx, sqlDB, db.SQLite, allMigrations); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fmt.Println("rolled back last migration")
			return 0
		}
		if err := migration.ApplyPending(ctx, sqlDB, db.SQLite, allMigrations); err != nil {
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
	registry.SetStore(store)

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

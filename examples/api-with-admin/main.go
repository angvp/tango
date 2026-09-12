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

	"api-with-admin/apps/posts"
	"api-with-admin/migrations"

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

	// sql.Open only validates the DSN; it doesn't dial the database, so it's
	// safe to construct the store here and share it across every flag path
	// (including -check/-tango-dump-models, which never touch it).
	sqlDB, err := sql.Open("sqlite", "app.db")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, db.SQLite)

	config := tango.Config{
		InstalledApps: []tango.App{
			posts.New(store),
			admin.New(store, admin.Credentials{
				Username: "admin",
				Password: "change-me",
			}),
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

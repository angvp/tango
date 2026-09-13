// Command reusable-greetings is a throwaway generation/dev-check harness
// that lives inside the greetings app's own repo, per the contributed
// migrations convention documented in docs/guides/reusable-apps.md. It
// exists only so `tango check` / `tango makemigrations` have something to
// run against — it is never the app's real host. Its Config is scoped to
// this app only: it never defines or overrides host-level config such as a
// real DB dialect/DSN.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"

	"reusable-greetings/greetings"

	_ "modernc.org/sqlite"
)

func main() {
	os.Exit(run())
}

func run() int {
	check := flag.Bool("check", false, "validate app registration and exit")
	dumpModels := flag.Bool("tango-dump-models", false, "print registered models as JSON and exit")
	flag.Parse()

	// A real but throwaway in-memory store — DumpModels performs no DB I/O,
	// but the app's constructor still expects a *db.Store, and db.NewStore's
	// nil-safety isn't a documented guarantee.
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, db.SQLite)

	config := tango.Config{
		InstalledApps: []tango.App{greetings.New(store)},
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

	fmt.Fprintln(os.Stderr, "this is a generation/dev-check harness, not a runnable server — pass -check or -tango-dump-models")
	return 1
}

package tango_test

// This file enforces the contributed-migrations decision: a reusable app
// ships its own Migrations var, generated via a throwaway generation
// harness inside its own repo, and the host's main.go concatenates it with
// its own migrations before applying — see docs/guides/reusable-apps.md.

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestContributedMigrationsApplyInHostProject(t *testing.T) {
	host := buildHostExample(t)
	host.run(t, "-migrate")

	statusOut := host.run(t, "-tango-status")
	// The host's own migrations.Migrations is empty, so the one applied
	// migration reported here can only be the contributed one concatenated
	// in from the greetings app's own Migrations var.
	if !strings.Contains(statusOut, `"migrationsApplied":1`) {
		t.Fatalf("tango-status does not show exactly one applied migration (expected the contributed greetings migration):\n%s", statusOut)
	}

	// This reads the host app's own SQLite database, written by the runs
	// above, not a test database: the run's Test dialect does not apply to
	// it.
	sqlDB, err := sql.Open("sqlite", host.dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", host.dbPath, err)
	}
	defer sqlDB.Close()

	var app, name string
	if err := sqlDB.QueryRow(`SELECT app, name FROM tango_migrations`).Scan(&app, &name); err != nil {
		t.Fatalf("query tango_migrations in %s: %v", host.dbPath, err)
	}
	if app != "greetings" || name != "0001_auto" {
		t.Fatalf("tango_migrations recorded (%q, %q), want (%q, %q) — the contributed migration from the greetings app", app, name, "greetings", "0001_auto")
	}
}

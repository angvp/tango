package tango_test

// This file enforces the contributed-migrations decision: a reusable app
// ships its own Migrations var, generated via a throwaway generation
// harness inside its own repo, and the host's main.go concatenates it with
// its own migrations before applying — see docs/guides/reusable-apps.md.

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestContributedMigrationsApplyInHostProject(t *testing.T) {
	const hostDir = "examples/reusable-greetings-host"
	dbPath := filepath.Join(hostDir, "app.db")

	_ = os.Remove(dbPath)
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	cmd := exec.Command("go", "run", ".", "-migrate")
	cmd.Dir = hostDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -migrate in %s failed: %v\n%s", hostDir, err, out)
	}

	statusCmd := exec.Command("go", "run", ".", "-tango-status")
	statusCmd.Dir = hostDir
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -tango-status in %s failed: %v\n%s", hostDir, err, statusOut)
	}
	// The host's own migrations.Migrations is empty, so the one applied
	// migration reported here can only be the contributed one concatenated
	// in from the greetings app's own Migrations var.
	if !strings.Contains(string(statusOut), `"migrationsApplied":1`) {
		t.Fatalf("tango-status does not show exactly one applied migration (expected the contributed greetings migration):\n%s", statusOut)
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer sqlDB.Close()

	var app, name string
	if err := sqlDB.QueryRow(`SELECT app, name FROM tango_migrations`).Scan(&app, &name); err != nil {
		t.Fatalf("query tango_migrations in %s: %v", dbPath, err)
	}
	if app != "greetings" || name != "0001_auto" {
		t.Fatalf("tango_migrations recorded (%q, %q), want (%q, %q) — the contributed migration from the greetings app", app, name, "greetings", "0001_auto")
	}
}

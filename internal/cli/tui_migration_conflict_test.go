package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/angvp/tango"
)

const conflictMessage = "tango migration: conflicting migration names:\n- app \"blog\": number 0005 is shared by \"0005_add_email\", \"0005_add_phone\"; renumber one migration"

func TestPrintStatusPlainShowsMigrationConflict(t *testing.T) {
	var out strings.Builder
	printStatusPlain(&out, tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationError:    conflictMessage,
	})

	got := out.String()
	if !strings.Contains(got, "migrations: CONFLICT ("+conflictMessage+")") {
		t.Fatalf("output does not show the migration conflict:\n%s", got)
	}
	if !strings.Contains(got, "registration: ok") {
		t.Fatalf("output lost the registration line:\n%s", got)
	}
}

func TestPrintStatusPlainOmitsMigrationLineWithoutConflict(t *testing.T) {
	var out strings.Builder
	printStatusPlain(&out, tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true})

	if strings.Contains(out.String(), "CONFLICT") {
		t.Fatalf("output shows a conflict for a valid registry:\n%s", out.String())
	}
}

// lastPlainLine returns the final line printStatusPlain wrote, which is the
// actions line.
func lastPlainLine(t *testing.T, status tango.ProjectStatus) string {
	t.Helper()
	var out strings.Builder
	printStatusPlain(&out, status)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	return lines[len(lines)-1]
}

func TestPrintStatusPlainActionsOmitMigrateOnConflict(t *testing.T) {
	got := lastPlainLine(t, tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationError: conflictMessage})
	const want = "actions available via: tango run | tango migrate down"
	if got != want {
		t.Fatalf("actions line = %q, want %q", got, want)
	}
}

func TestPrintStatusPlainActionsListMigrateWithoutConflict(t *testing.T) {
	got := lastPlainLine(t, tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true})
	const want = "actions available via: tango run | tango migrate | tango migrate down"
	if got != want {
		t.Fatalf("actions line = %q, want %q", got, want)
	}
}

func TestDashboardViewShowsMigrationConflict(t *testing.T) {
	model := newDashboardModel(tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationError:    conflictMessage,
	})

	view := model.View()
	if !strings.Contains(view, "migrations: CONFLICT ("+conflictMessage+")") {
		t.Fatalf("dashboard does not show the migration conflict:\n%s", view)
	}
}

func TestApplyMenuItemUnavailableOnConflict(t *testing.T) {
	ok, reason := menuApplyMigrations.availability(tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationError:    conflictMessage,
		MigrationsPending: 2,
	})
	if ok || reason != reasonMigrationConflict {
		t.Fatalf("availability = (%v, %q), want (false, %q)", ok, reason, reasonMigrationConflict)
	}
}

func TestPlainActionsLineOmitsMigrateActionOnConflict(t *testing.T) {
	line := lastPlainLine(t, tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationError: conflictMessage})
	const prefix = "actions available via: "
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("actions line %q lacks prefix %q", line, prefix)
	}
	for _, action := range strings.Split(strings.TrimPrefix(line, prefix), " | ") {
		if action == "tango migrate" {
			t.Fatalf("actions line offers %q on a conflict: %q", action, line)
		}
	}
}

func TestMigrateActionsKeptWithoutConflict(t *testing.T) {
	status := tango.ProjectStatus{RegistrationOK: true, DatabaseReachable: true, MigrationsTotal: 2, MigrationsPending: 2}

	if ok, reason := menuApplyMigrations.availability(status); !ok {
		t.Fatalf("apply unavailable without conflict: %q", reason)
	}
	line := lastPlainLine(t, status)
	if !strings.Contains(line, " | tango migrate | ") {
		t.Fatalf("actions line %q lacks the standalone migrate action", line)
	}
}

func TestDashboardViewShowsConflictReasonNextToApply(t *testing.T) {
	model := newDashboardModel(tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationError:    conflictMessage,
		MigrationsPending: 2,
	})

	view := model.View()
	if !strings.Contains(view, "Apply pending migrations ("+reasonMigrationConflict+")") {
		t.Fatalf("dashboard does not show the conflict reason next to Apply:\n%s", view)
	}
	const literal = "Apply pending migrations (unavailable: migration registry has a conflict)"
	if !strings.Contains(view, literal) {
		t.Fatalf("dashboard reason text changed; want %q in:\n%s", literal, view)
	}
	if !strings.Contains(view, "migrations: CONFLICT (") {
		t.Fatalf("dashboard lost the CONFLICT line:\n%s", view)
	}
}

// The TUI reads status from the -tango-status JSON through tui(), so this
// drives the same path a user takes in a non-interactive shell: the JSON the
// project prints must come out as the conflict line, with exit 0.
func TestTUIPlainFallbackShowsMigrationConflictFromStatusJSON(t *testing.T) {
	dir := t.TempDir()
	encoded, err := json.Marshal(tango.ProjectStatus{
		RegistrationOK:    true,
		DatabaseReachable: true,
		MigrationError:    conflictMessage,
		MigrationsTotal:   2,
		MigrationsPending: 2,
	})
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	runner := &jsonStdoutRunner{payload: encoded}

	var stdout, stderr strings.Builder
	code := tui(context.Background(), runner, dir, &stdout, &stderr, func() bool { return false }, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "migrations: CONFLICT (") {
		t.Fatalf("stdout does not show the conflict:\n%s", stdout.String())
	}
}

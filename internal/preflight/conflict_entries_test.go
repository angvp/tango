package preflight

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

// flagNamesFromUsage runs DispatchFlags with -h and returns the flag names its
// usage prints, so the list comes from the parser and not from this file.
func flagNamesFromUsage(t *testing.T) []string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	var helpErr error
	withArgs(t, []string{"tango", "-h"}, func() {
		_, helpErr = tango.DispatchFlags(tango.Config{}, nil, db.SQLite, nil)
	})
	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatalf("closing stderr pipe: %v", err)
	}
	usage, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading usage: %v", err)
	}
	if !errors.Is(helpErr, flag.ErrHelp) {
		t.Fatalf("-h err = %v, want flag.ErrHelp", helpErr)
	}
	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(usage))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "-") {
			names = append(names, strings.TrimLeft(strings.Fields(line)[0], "-"))
		}
	}
	return names
}

// Each entry applies, reports or checks migrations and must surface the
// conflict. Status carries it in a field, not in its error.
func TestPublicMigrationEntriesRefuseConflict(t *testing.T) {
	for _, tc := range conflictCases() {
		t.Run(tc.name+"/ApplyPending", func(t *testing.T) {
			sqlDB, log := newRecordingDB(t)
			err := migration.ApplyPending(context.Background(), sqlDB, db.SQLite, tc.registry)
			if !errors.Is(err, migration.ErrConflictingMigrations) {
				t.Fatalf("err = %v, want ErrConflictingMigrations", err)
			}
			if calls := log.snapshot(); len(calls) != 0 {
				t.Fatalf("calls = %v, want none", calls)
			}
		})
		t.Run(tc.name+"/Status", func(t *testing.T) {
			sqlDB, _ := newRecordingDB(t)
			status := tango.Status(context.Background(), tango.Config{}, sqlDB, db.SQLite, tc.registry)
			want := strings.TrimPrefix(tc.checkErr, "check failed: ")
			if status.MigrationError != want {
				t.Fatalf("MigrationError = %q, want %q", status.MigrationError, want)
			}
		})
	}
}

// outcomeKind names what a flag pair must do under a conflicting registry.
type outcomeKind int

const (
	// refuse: the sentinel is returned and nothing is written.
	refuse outcomeKind = iota
	// statusJSON: the flag path reports the conflict in its JSON output.
	statusJSON
	// precedenceError: the other flag wins and returns a non-sentinel error
	// (no OpenAPI app is installed), with nothing written.
	precedenceError
	// rollbackExempt: -down is the deliberate recovery exemption. It ignores
	// the conflict and reports the empty history the same way a valid
	// registry does. It may create the tracking table, so exec is allowed.
	rollbackExempt
)

type pairOutcome struct {
	kind outcomeKind
}

// pairOutcomes is keyed "primary other" and must hold one entry for every
// pair the parser's flag set produces with -migrate or -check.
var pairOutcomes = map[string]pairOutcome{
	"migrate check":             {kind: refuse},
	"migrate down":              {kind: rollbackExempt},
	"migrate tango-status":      {kind: statusJSON},
	"migrate tango-dump-models": {kind: refuse},
	"migrate tango-openapi":     {kind: precedenceError},
	"check migrate":             {kind: refuse},
	"check down":                {kind: refuse},
	"check tango-status":        {kind: refuse},
	"check tango-dump-models":   {kind: refuse},
	"check tango-openapi":       {kind: precedenceError},
}

// Combined flags may change which path runs, but no pair may apply a write
// under a conflicting registry. The pairs come from the parser's own usage.
func TestFlagPairsNeverApplyUnderConflict(t *testing.T) {
	names := flagNamesFromUsage(t)
	if len(names) == 0 {
		t.Fatal("usage listed no flags")
	}
	conflict := conflictCases()[1]
	for _, primary := range []string{"migrate", "check"} {
		for _, other := range names {
			if other == primary {
				continue
			}
			key := primary + " " + other
			t.Run(key, func(t *testing.T) {
				outcome, ok := pairOutcomes[key]
				if !ok {
					t.Fatalf("no expected outcome for %q; add it to pairOutcomes", key)
				}
				sqlDB, log := newRecordingDB(t)
				var handled bool
				var runErr error
				output, _ := captureCheckStdout(t, func() error {
					withArgs(t, []string{"tango", "-" + primary, "-" + other}, func() {
						handled, runErr = tango.DispatchFlags(tango.Config{}, sqlDB, db.SQLite, conflict.registry)
					})
					return runErr
				})
				if !handled {
					t.Fatal("handled = false, want true")
				}
				if outcome.kind != rollbackExempt {
					for _, call := range log.snapshot() {
						if call == "exec" || call == "stmt-exec" {
							t.Fatalf("write %q under a conflicting registry; calls = %v", call, log.snapshot())
						}
					}
				}
				switch outcome.kind {
				case refuse:
					if !errors.Is(runErr, migration.ErrConflictingMigrations) {
						t.Fatalf("err = %v, want ErrConflictingMigrations", runErr)
					}
				case statusJSON:
					if runErr != nil {
						t.Fatalf("err = %v, want none; status reports the conflict in JSON", runErr)
					}
					var status tango.ProjectStatus
					if err := json.Unmarshal([]byte(output), &status); err != nil {
						t.Fatalf("status output is not JSON: %v\n%s", err, output)
					}
					want := strings.TrimPrefix(conflict.checkErr, "check failed: ")
					if status.MigrationError != want {
						t.Fatalf("MigrationError = %q, want %q", status.MigrationError, want)
					}
				case precedenceError:
					if runErr == nil || errors.Is(runErr, migration.ErrConflictingMigrations) {
						t.Fatalf("err = %v, want a non-conflict error from the precedence flag", runErr)
					}
				case rollbackExempt:
					if errors.Is(runErr, migration.ErrConflictingMigrations) {
						t.Fatalf("err = %v, -down must be exempt from the registry check", runErr)
					}
					if !errors.Is(runErr, migration.ErrNoAppliedMigrations) {
						t.Fatalf("err = %v, want ErrNoAppliedMigrations on an empty database", runErr)
					}
				}
			})
		}
	}
}

// TestMigrateDownIsRecoveryExemptFromConflict pins decision D1: -migrate -down
// is the deliberate recovery exemption from registry validation. The positive
// control runs the same empty-database path with a valid registry and must
// produce the same sentinel, so the conflicting run cannot pass by accident
// through some other error.
func TestMigrateDownIsRecoveryExemptFromConflict(t *testing.T) {
	cases := []struct {
		name     string
		registry []migration.Migration
	}{
		{name: "positive control: valid registry on fresh database", registry: validMigrations()},
		{name: "recovery exemption: conflicting registry on fresh database", registry: conflictCases()[1].registry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, _ := newRecordingDB(t)
			var runErr error
			withArgs(t, []string{"tango", "-migrate", "-down"}, func() {
				_, runErr = tango.DispatchFlags(tango.Config{}, sqlDB, db.SQLite, tc.registry)
			})
			if !errors.Is(runErr, migration.ErrNoAppliedMigrations) {
				t.Fatalf("err = %v, want ErrNoAppliedMigrations", runErr)
			}
			if errors.Is(runErr, migration.ErrConflictingMigrations) {
				t.Fatalf("err = %v, -down must not return ErrConflictingMigrations", runErr)
			}
		})
	}
}

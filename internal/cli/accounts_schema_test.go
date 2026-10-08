package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

// TestAccountsMailSchemaIsAnAdditiveUpgrade upgrades an app whose accounts
// tables were generated from v0.1.0's models (testdata, recorded before
// the change) to today's: makemigrations needs no --rename or
// --allow-drop, generates only the new column and table, and the result
// applies on the run's Test dialect.
func TestAccountsMailSchemaIsAnAdditiveUpgrade(t *testing.T) {
	dir := t.TempDir()
	before, err := os.ReadFile(filepath.Join("testdata", "accounts_models_v0_1_0.json"))
	if err != nil {
		t.Fatal(err)
	}
	store := testdb.Store(t)
	models, err := tango.DumpModels(tango.Config{InstalledApps: []tango.App{accounts.New(store)}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}

	for i, dump := range [][]byte{before, after} {
		var stderr strings.Builder
		if code := Run(context.Background(), []string{"makemigrations"}, dir, io.Discard, &stderr, rawDumpRunner{output: dump}); code != 0 {
			t.Fatalf("makemigrations %d exit code = %d: %s", i+1, code, stderr.String())
		}
	}
	migrations, err := loadMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 {
		t.Fatalf("migrations = %d, want the initial one and the upgrade", len(migrations))
	}

	var added []string
	for _, step := range migrations[1].Up {
		switch s := step.(type) {
		case migration.AddColumn:
			added = append(added, "column "+s.Table+"."+s.Column.Name+" "+s.Column.Type)
		case migration.CreateTable:
			added = append(added, "table "+s.Table)
		case migration.CreateIndex:
			added = append(added, "index on "+s.Table)
		default:
			t.Fatalf("the upgrade contains %T %+v, want only additions", step, step)
		}
	}
	want := map[string]bool{"column account.email_verified_at timestamp": true, "table account_token": true}
	for _, a := range added {
		if strings.HasPrefix(a, "index on account_token") {
			continue
		}
		if !want[a] {
			t.Fatalf("the upgrade adds %q; all additions: %q", a, added)
		}
		delete(want, a)
	}
	if len(want) != 0 {
		t.Fatalf("the upgrade lacks %v; it adds %q", want, added)
	}

	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(context.Background(), sqlDB, dialect, migrations); err != nil {
		t.Fatalf("applying: %v", err)
	}

	var stdout strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, io.Discard, rawDumpRunner{output: after}); code != 0 {
		t.Fatalf("third makemigrations exit code = %d", code)
	}
	if again, _ := loadMigrations(dir); len(again) != 2 {
		t.Fatalf("a third makemigrations generated more: %d migrations", len(again))
	}
}

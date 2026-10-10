package tango_test

// The JSON-field guide snippets are real code: the blocks that follow the
// `<!-- snippet: json-field -->` and `<!-- snippet: json-field-audit -->`
// markers in docs/guides/models-and-tags.md must appear (ignoring
// whitespace) in this file, and this file runs them.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

type Settings struct {
	ID    int64  `tango:"pk"`
	Owner string `tango:"varchar=60"`
	Prefs string `tango:"json"`
}

func invalidSettings(ctx context.Context, store *db.Store, meta model.ModelMeta) ([]int64, error) {
	var rows []Settings
	if err := store.List(ctx, meta, db.Query{OrderBy: []string{"ID"}}, &rows); err != nil {
		return nil, err
	}
	var bad []int64
	for _, row := range rows {
		if row.Prefs != "" && !json.Valid([]byte(row.Prefs)) {
			bad = append(bad, row.ID)
		}
	}
	return bad, nil
}

func jsonGuideSnippet(t *testing.T, marker string) string {
	t.Helper()
	guide, err := os.ReadFile("docs/guides/models-and-tags.md")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile("(?s)<!-- snippet: " + regexp.QuoteMeta(marker) + " -->\n```go\n(.*?)\n```")
	match := pattern.FindSubmatch(guide)
	if match == nil {
		t.Fatalf("docs/guides/models-and-tags.md has no `<!-- snippet: %s -->` block", marker)
	}
	return string(match[1])
}

func TestJSONFieldGuideSnippetsAreThisFilesCode(t *testing.T) {
	self, err := os.ReadFile("json_field_guide_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"json-field", "json-field-audit"} {
		snippet := jsonGuideSnippet(t, marker)
		if !strings.Contains(squash(string(self)), squash(snippet)) {
			t.Errorf("the guide's %s snippet no longer appears in json_field_guide_test.go:\n%s", marker, snippet)
		}
	}
}

func TestJSONFieldGuideBehavesAsTheGuideSays(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(Settings{}); err != nil {
		t.Fatalf("the guide's model does not register: %v", err)
	}
	meta, _ := registry.Models().Get("Settings")
	flags := map[string]bool{}
	for _, field := range meta.Fields {
		flags[field.Name] = field.JSON
	}
	if !reflect.DeepEqual(flags, map[string]bool{"ID": false, "Owner": false, "Prefs": true}) {
		t.Fatalf("FieldMeta.JSON = %v, want only Prefs", flags)
	}

	migrations, err := migration.Diff(registry.Models().All(), migration.SchemaState{Tables: map[string]migration.TableState{}})
	if err != nil {
		t.Fatal(err)
	}
	var prefs migration.Column
	for _, column := range migrations[0].Up[0].(migration.CreateTable).Columns {
		if column.Name == "prefs" {
			prefs = column
		}
	}
	if prefs.Type != "text" || prefs.Length != 0 || prefs.Unique || prefs.Indexed {
		t.Fatalf("prefs column = %+v, want plain text", prefs)
	}

	sqlDB, dialect := testdb.Open(t)
	store := db.NewStore(sqlDB, dialect)
	for _, step := range migrations[0].Up {
		if err := migration.ApplyStep(context.Background(), sqlDB, dialect, step); err != nil {
			t.Fatal(err)
		}
	}
	store.UseModels(registry.Models())
	ctx := context.Background()

	err = store.Create(ctx, meta, &Settings{Owner: "ada", Prefs: `{"theme":`})
	var invalid *db.InvalidJSONError
	if !errors.Is(err, db.ErrInvalidJSON) || !errors.As(err, &invalid) || invalid.Field != "Prefs" {
		t.Fatalf("Create error = %v, want an InvalidJSONError for Prefs", err)
	}
	for _, body := range []string{"", "null", `{ "theme" : "dark" }`} {
		if err := store.Create(ctx, meta, &Settings{Owner: "ada", Prefs: body}); err != nil {
			t.Fatalf("Create with %q: %v", body, err)
		}
	}

	// The audit loop finds a row that raw SQL (or a time before the tag
	// existed) left invalid, and leaves valid, null and unset rows alone.
	if _, err := sqlDB.Exec(`INSERT INTO settings (owner, prefs) VALUES ('old', 'not json')`); err != nil {
		t.Fatal(err)
	}
	bad, err := invalidSettings(ctx, store, meta)
	if err != nil || len(bad) != 1 || bad[0] != 4 {
		t.Fatalf("invalidSettings = %v (err %v), want only row 4", bad, err)
	}
}

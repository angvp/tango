package tango_test

// The bounded-strings guide snippet is real code: the block that follows the
// `<!-- snippet: bounded-strings -->` marker in docs/guides/models-and-tags.md
// must appear (ignoring whitespace) in this file, and this file runs it. The
// same pattern as the tutorial snippet check keeps the guide honest.

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

type Headline struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=200"`
	Slug  string `tango:"varchar=80,unique"`
	Body  string `tango:"text"`
}

var snippetBlock = regexp.MustCompile("(?s)<!-- snippet: bounded-strings -->\n```go\n(.*?)\n```")

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestBoundedStringGuideSnippetIsThisFilesModel(t *testing.T) {
	guide, err := os.ReadFile("docs/guides/models-and-tags.md")
	if err != nil {
		t.Fatal(err)
	}
	match := snippetBlock.FindSubmatch(guide)
	if match == nil {
		t.Fatal("docs/guides/models-and-tags.md has no `<!-- snippet: bounded-strings -->` block")
	}
	self, err := os.ReadFile("bounded_string_guide_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(squash(string(self)), squash(string(match[1]))) {
		t.Fatalf("the guide's bounded-strings snippet no longer appears in bounded_string_guide_test.go:\n%s", match[1])
	}
}

func TestBoundedStringGuideSnippetBehavesAsTheGuideSays(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(Headline{}); err != nil {
		t.Fatalf("the guide's model does not register: %v", err)
	}
	meta, _ := registry.Models().Get("Headline")
	limits := map[string]int{}
	for _, field := range meta.Fields {
		limits[field.Name] = field.MaxLength
	}
	if limits["Title"] != 200 || limits["Slug"] != 80 || limits["Body"] != 0 || limits["ID"] != 0 {
		t.Fatalf("MaxLength = %v, want Title 200, Slug 80, Body and ID 0", limits)
	}

	migrations, err := migration.Diff(registry.Models().All(), migration.SchemaState{Tables: map[string]migration.TableState{}})
	if err != nil {
		t.Fatal(err)
	}
	create := migrations[0].Up[0].(migration.CreateTable)
	got := map[string]migration.Column{}
	for _, column := range create.Columns {
		got[column.Name] = column
	}
	if got["title"].Type != "varchar" || got["title"].Length != 200 || got["slug"].Unique != true || got["body"].Type != "text" || got["body"].Length != 0 {
		t.Fatalf("columns = %+v", got)
	}

	sqlDB, dialect := testdb.Open(t)
	store := db.NewStore(sqlDB, dialect)
	for _, step := range migrations[0].Up {
		if err := migration.ApplyStep(context.Background(), sqlDB, dialect, step); err != nil {
			t.Fatal(err)
		}
	}
	store.UseModels(registry.Models())
	err = store.Create(context.Background(), meta, &Headline{Title: strings.Repeat("é", 201), Slug: "a"})
	var tooLong *db.ValueTooLongError
	if !errors.Is(err, db.ErrValueTooLong) || !errors.As(err, &tooLong) || tooLong.Field != "Title" || tooLong.Max != 200 || tooLong.Got != 201 {
		t.Fatalf("Create error = %v, want a ValueTooLongError for Title (201 of 200)", err)
	}
	if err := store.Create(context.Background(), meta, &Headline{Title: strings.Repeat("é", 200), Slug: "a"}); err != nil {
		t.Fatalf("a title of exactly 200 runes: %v", err)
	}
}

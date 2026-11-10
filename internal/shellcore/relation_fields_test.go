package shellcore

import (
	"context"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/testdb"
)

type RelAuthor struct {
	ID   int64  `tango:"pk"`
	Name string `tango:"varchar=100"`
}

type RelPost struct {
	ID       int64      `tango:"pk"`
	AuthorID int64      `tango:"fk=RelAuthor"`
	Title    string     `tango:"varchar=200"`
	Author   *RelAuthor `tango:"belongs_to=AuthorID"`
}

func newRelationVerbs(t *testing.T) *Verbs {
	t.Helper()
	blog := tango.NewApp("blog", func(r *tango.Registry) error {
		if err := r.Models().Register(RelAuthor{}); err != nil {
			return err
		}
		return r.Models().Register(RelPost{})
	})
	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{blog}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, registry.Models().All())
	store := db.NewStore(sqlDB, dialect)
	registry.SetStore(store)
	return NewVerbs(context.Background(), registry.Models(), store, false)
}

// The shell lists columns from ModelMeta.Fields, so a relation field is absent
// from Describe and from every row. The Title column is the known positive: a
// real field of the same model does appear.
func TestShellShowsNoRelationFieldAndStillShowsColumns(t *testing.T) {
	verbs := newRelationVerbs(t)

	rows, err := verbs.Describe("blog.RelPost")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, row := range rows {
		names[row["Name"].(string)] = true
	}
	if names["Author"] {
		t.Fatalf("Describe lists the relation field Author: %v", rows)
	}
	if !names["Title"] || !names["AuthorID"] {
		t.Fatalf("Describe = %v, want the Title and AuthorID columns", rows)
	}
	if len(rows) != 3 {
		t.Fatalf("Describe has %d rows, want 3 columns (ID, AuthorID, Title)", len(rows))
	}

	author, err := verbs.Create("blog.RelAuthor", map[string]any{"Name": "Ada"})
	if err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post, err := verbs.Create("blog.RelPost", map[string]any{"AuthorID": author["ID"], "Title": "Hello"})
	if err != nil {
		t.Fatalf("Create post: %v", err)
	}
	got, err := verbs.Get("blog.RelPost", post["ID"])
	if err != nil {
		t.Fatalf("Get post: %v", err)
	}
	if _, present := got["Author"]; present {
		t.Fatalf("Get row carries the relation field Author: %v", got)
	}
	if got["Title"] != "Hello" {
		t.Fatalf("Get row Title = %v, want Hello", got["Title"])
	}
}

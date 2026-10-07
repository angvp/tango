package migration

import (
	"errors"
	"strings"
	"testing"
)

// postState is migration history holding an author table and a post table
// whose author_id references it.
func postState() SchemaState {
	return SchemaState{Tables: map[string]TableState{
		"author": {Name: "author", App: "posts", Columns: []ColumnState{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
		"editor": {Name: "editor", App: "posts", Columns: []ColumnState{
			{Name: "id", Type: "integer", PrimaryKey: true},
		}},
		"post": {Name: "post", App: "posts", Columns: []ColumnState{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "author_id", Type: "integer", References: "author"},
			{Name: "slug", Type: "text"},
			{Name: "views", Type: "integer"},
		}},
	}}
}

// postModels is the models matching postState, with change applied to the
// post model's columns.
func postModels(change func(columns []Column)) []Model {
	post := []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "author"},
		{Name: "slug", Type: "text"},
		{Name: "views", Type: "integer"},
	}
	change(post)
	fields := map[string]string{"id": "ID", "author_id": "AuthorID", "slug": "Slug", "views": "Views"}
	return []Model{
		{App: "posts", Name: "author", Struct: "Author", Fields: map[string]string{"id": "ID"}, Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		{App: "posts", Name: "editor", Struct: "Editor", Fields: map[string]string{"id": "ID"}, Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		{App: "posts", Name: "post", Struct: "Post", Fields: fields, Columns: post},
	}
}

func TestDiffRefusesChangesAMigrationCannotExpress(t *testing.T) {
	tests := []struct {
		name   string
		change func(columns []Column)
		want   []string
	}{
		{
			name:   "type change",
			change: func(c []Column) { c[3].Type = "text" },
			want:   []string{"posts.Post.Views", "integer", "text"},
		},
		{
			name:   "primary key moves",
			change: func(c []Column) { c[0].PrimaryKey = false; c[2].PrimaryKey = true },
			want:   []string{"posts.Post.ID", "posts.Post.Slug", "primary key"},
		},
		{
			name:   "foreign key target changes",
			change: func(c []Column) { c[1].References = "editor" },
			want:   []string{"posts.Post.AuthorID", "author", "editor"},
		},
		{
			name:   "foreign key added to an existing field",
			change: func(c []Column) { c[3].References = "author" },
			want:   []string{"posts.Post.Views", "foreign key"},
		},
		{
			name:   "foreign key removed",
			change: func(c []Column) { c[1].References = "" },
			want:   []string{"posts.Post.AuthorID", "foreign key"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migrations, err := DiffModels(postModels(tt.change), postState())
			if !errors.Is(err, ErrUnsupportedChange) {
				t.Fatalf("DiffModels error = %v, want ErrUnsupportedChange", err)
			}
			if migrations != nil {
				t.Fatalf("DiffModels returned migrations %v alongside an error, want none", migrations)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestDiffReportsEveryUnsupportedChangeAtOnce(t *testing.T) {
	_, err := DiffModels(postModels(func(c []Column) {
		c[3].Type = "real"
		c[1].References = "editor"
	}), postState())
	for _, want := range []string{"posts.Post.Views", "posts.Post.AuthorID"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
}

func TestDiffTreatsTheSameColumnTypeAsNoChange(t *testing.T) {
	// int and int64 fields both map to "integer": the models match history.
	migrations, err := DiffModels(postModels(func([]Column) {}), postState())
	if err != nil || len(migrations) != 0 {
		t.Fatalf("DiffModels = %v, %v; want no migrations and no error", migrations, err)
	}
}

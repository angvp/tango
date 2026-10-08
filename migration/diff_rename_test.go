package migration

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// articleState is history with a posts app's post table.
func articleState() SchemaState {
	return SchemaState{Tables: map[string]TableState{
		"post": {Name: "post", App: "posts", Columns: []ColumnState{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "title", Type: "text"},
			{Name: "body", Type: "text", Indexed: true},
		}},
	}}
}

// articleModels is the post model after its Body field became Content,
// with change applied to its columns.
func articleModels(change func(columns []Column)) []Model {
	columns := []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "text"},
		{Name: "content", Type: "text", Indexed: true},
	}
	change(columns)
	return []Model{{
		App: "posts", Name: "post", Struct: "Post", Columns: columns,
		Fields: map[string]string{"id": "ID", "title": "Title", "content": "Content"},
	}}
}

func noChange([]Column) {}

func TestDiffTurnsARenamedFieldIntoRenameColumn(t *testing.T) {
	migrations, err := DiffModels(articleModels(noChange), articleState(),
		Rename{App: "posts", Table: "post", Column: "body", To: "content"})
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	want := []Migration{{
		App:        "posts",
		Up:         []Step{RenameColumn{Table: "post", From: "body", To: "content"}},
		Down:       []Step{RenameColumn{Table: "post", From: "content", To: "body"}},
		Reversible: true,
	}}
	if !reflect.DeepEqual(migrations, want) {
		t.Fatalf("migrations = %#v, want %#v", migrations, want)
	}
}

func TestDiffRenamesBeforeChangingTheRenamedField(t *testing.T) {
	migrations, err := DiffModels(articleModels(func(c []Column) { c[2].Unique = true }), articleState(),
		Rename{App: "posts", Table: "post", Column: "body", To: "content"})
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	wantUp := []Step{
		RenameColumn{Table: "post", From: "body", To: "content"},
		AlterColumnUnique{Table: "post", Column: "content", Unique: true},
	}
	if len(migrations) != 1 || !reflect.DeepEqual(migrations[0].Up, wantUp) {
		t.Fatalf("migrations = %#v, want one with Up %#v", migrations, wantUp)
	}
}

func TestReplayedRenameLeavesNothingToDiff(t *testing.T) {
	renamed, err := DiffModels(articleModels(noChange), articleState(),
		Rename{App: "posts", Table: "post", Column: "body", To: "content"})
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	history := []Migration{{App: "posts", Name: "0001_initial", Up: []Step{CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "text"},
		{Name: "body", Type: "text", Indexed: true},
	}}}}}
	renamed[0].Name = "0002_rename"
	state, err := Replay(append(history, renamed...))
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	again, err := DiffModels(articleModels(noChange), state)
	if err != nil || len(again) != 0 {
		t.Fatalf("DiffModels after the rename = %#v, %v; want no migrations", again, err)
	}
}

func TestReplayRejectsARenameItCannotApply(t *testing.T) {
	create := Migration{App: "posts", Name: "0001_initial", Up: []Step{CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "body", Type: "text"},
	}}}}
	for _, rename := range []RenameColumn{
		{Table: "post", From: "missing", To: "content"},
		{Table: "post", From: "body", To: "id"},
		{Table: "nothing", From: "body", To: "content"},
	} {
		_, err := Replay([]Migration{create, {App: "posts", Name: "0002", Up: []Step{rename}}})
		if err == nil {
			t.Fatalf("Replay accepted %#v, want an error", rename)
		}
	}
}

func TestDiffRejectsRenameMappingsThatDoNotFit(t *testing.T) {
	tests := []struct {
		name    string
		models  []Model
		renames []Rename
		wants   []string
	}{
		{
			name:    "source not in history",
			models:  articleModels(noChange),
			renames: []Rename{{App: "posts", Table: "post", Column: "bodie", To: "content"}},
			wants:   []string{"posts.post.bodie", "not in migration history", "id, title, body"},
		},
		{
			name:    "source still in the models",
			models:  articleModels(func(c []Column) { c[2].Name = "body" }),
			renames: []Rename{{App: "posts", Table: "post", Column: "title", To: "heading"}},
			wants:   []string{"posts.post.title", "still has", "Title"},
		},
		{
			name:    "destination not in the models",
			models:  articleModels(noChange),
			renames: []Rename{{App: "posts", Table: "post", Column: "body", To: "contents"}},
			wants:   []string{"contents", "no field", "ID, Title, Content"},
		},
		{
			name:    "destination already in history",
			models:  articleModels(noChange),
			renames: []Rename{{App: "posts", Table: "post", Column: "body", To: "title"}},
			wants:   []string{"posts.post.title", "already in migration history"},
		},
		{
			name:    "table not in history",
			models:  articleModels(noChange),
			renames: []Rename{{App: "posts", Table: "story", Column: "body", To: "content"}},
			wants:   []string{"posts.story", "no table", "has tables post"},
		},
		{
			name:    "wrong app",
			models:  articleModels(noChange),
			renames: []Rename{{App: "blog", Table: "post", Column: "body", To: "content"}},
			wants:   []string{"blog.post.body", "app posts"},
		},
		{
			name:   "same source twice",
			models: articleModels(noChange),
			renames: []Rename{
				{App: "posts", Table: "post", Column: "body", To: "content"},
				{App: "posts", Table: "post", Column: "body", To: "contents"},
			},
			wants: []string{"posts.post.body", "more than once", "posts.post.content", "posts.post.contents"},
		},
		{
			name: "same destination twice",
			models: articleModels(func(c []Column) {
				c[1].Name = "content2"
			}),
			renames: []Rename{
				{App: "posts", Table: "post", Column: "body", To: "content"},
				{App: "posts", Table: "post", Column: "title", To: "content"},
			},
			wants: []string{"posts.post.content", "more than once", "posts.post.body", "posts.post.title"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migrations, err := DiffModels(tt.models, articleState(), tt.renames...)
			if !errors.Is(err, ErrInvalidRename) {
				t.Fatalf("DiffModels error = %v, want ErrInvalidRename", err)
			}
			if migrations != nil {
				t.Fatalf("DiffModels returned migrations alongside an error: %#v", migrations)
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

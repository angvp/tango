package migration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

// blogHistory is two apps: posts with a post table, and comments with a
// comment table referencing it.
func blogHistory() []Migration {
	return []Migration{
		{App: "posts", Name: "0001_initial", Reversible: true, Up: []Step{CreateTable{Table: "post", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "body", Type: "text", Indexed: true},
		}}}, Down: []Step{DropTable{Table: "post"}}},
		{App: "comments", Name: "0002_initial", Reversible: true, Up: []Step{CreateTable{Table: "comment", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "post_id", Type: "integer", References: "post"},
		}}}, Down: []Step{DropTable{Table: "comment"}}},
	}
}

// blogModels is the models after Post became Article; bodyName is the
// article's body column.
func blogModels(bodyName string) []Model {
	return []Model{
		{App: "posts", Name: "article", Struct: "Article", Fields: map[string]string{"id": "ID", bodyName: "Body"}, Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: bodyName, Type: "text", Indexed: true},
		}},
		{App: "comments", Name: "comment", Struct: "Comment", Fields: map[string]string{"id": "ID", "post_id": "PostID"}, Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "post_id", Type: "integer", References: "article"},
		}},
	}
}

var renamePostToArticle = Rename{App: "posts", Table: "post", To: "article"}

func TestDiffTurnsARenamedModelIntoRenameTable(t *testing.T) {
	migrations, err := DiffModels(blogModels("body"), mustReplay(t, blogHistory()...), renamePostToArticle)
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	want := []Migration{{
		App:        "posts",
		Up:         []Step{RenameTable{From: "post", To: "article"}},
		Down:       []Step{RenameTable{From: "article", To: "post"}},
		Reversible: true,
	}}
	if !reflect.DeepEqual(migrations, want) {
		t.Fatalf("migrations = %#v, want %#v (and no change to comments' foreign key)", migrations, want)
	}
}

func TestDiffRenamesAModelAndOneOfItsFieldsByTheModelsOldName(t *testing.T) {
	migrations, err := DiffModels(blogModels("content"), mustReplay(t, blogHistory()...),
		renamePostToArticle, Rename{App: "posts", Table: "post", Column: "body", To: "content"})
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	want := []Migration{{
		App: "posts",
		Up: []Step{
			RenameTable{From: "post", To: "article"},
			RenameColumn{Table: "article", From: "body", To: "content"},
		},
		Down: []Step{
			RenameColumn{Table: "article", From: "content", To: "body"},
			RenameTable{From: "article", To: "post"},
		},
		Reversible: true,
	}}
	if !reflect.DeepEqual(migrations, want) {
		t.Fatalf("migrations = %#v, want %#v", migrations, want)
	}
}

func TestReplayedModelRenameRewritesReferencesAndLeavesNothingToDiff(t *testing.T) {
	renamed, err := DiffModels(blogModels("body"), mustReplay(t, blogHistory()...), renamePostToArticle)
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	renamed[0].Name = "0003_rename"
	state := mustReplay(t, append(blogHistory(), renamed...)...)

	if _, stale := state.Tables["post"]; stale {
		t.Fatal("replayed state still has table post")
	}
	if got := state.Tables["article"]; got.Name != "article" || got.App != "posts" {
		t.Fatalf("replayed article table = %#v, want name article in app posts", got)
	}
	if got := state.Tables["comment"].Columns[1].References; got != "article" {
		t.Fatalf("comment.post_id references %q after replay, want %q", got, "article")
	}
	again, err := DiffModels(blogModels("body"), state)
	if err != nil || len(again) != 0 {
		t.Fatalf("DiffModels after the rename = %#v, %v; want no migrations", again, err)
	}
}

func TestReplayRejectsAModelRenameItCannotApply(t *testing.T) {
	for _, rename := range []RenameTable{{From: "missing", To: "article"}, {From: "post", To: "comment"}} {
		if _, err := Replay(append(blogHistory(), Migration{App: "posts", Name: "0003", Up: []Step{rename}})); err == nil {
			t.Fatalf("Replay accepted %#v, want an error", rename)
		}
	}
}

func TestDiffRejectsModelRenameMappingsThatDoNotFit(t *testing.T) {
	tests := []struct {
		name    string
		models  []Model
		renames []Rename
		wants   []string
	}{
		{"source not in history", blogModels("body"), []Rename{{App: "posts", Table: "story", To: "article"}}, []string{"posts.story", "posts.article", "no table", "has tables post"}},
		{"source still in the models", append(blogModels("body"), Model{App: "posts", Name: "post", Struct: "Post"}), []Rename{renamePostToArticle}, []string{"posts.post", "still has"}},
		{"destination not in the models", blogModels("body"), []Rename{{App: "posts", Table: "post", To: "articel"}}, []string{"posts.articel", "no model"}},
		{"destination already in history", blogModels("body"), []Rename{{App: "posts", Table: "post", To: "comment"}}, []string{"comment", "already in migration history"}},
		{"wrong app", blogModels("body"), []Rename{{App: "blog", Table: "post", To: "article"}}, []string{"blog.post", "app posts"}},
		{"same model twice", blogModels("body"), []Rename{renamePostToArticle, renamePostToArticle}, []string{"posts.post", "more than once"}},
		{"chained", blogModels("body"), []Rename{renamePostToArticle, {App: "posts", Table: "article", To: "story"}}, []string{"posts.article", "both"}},
		{
			name:    "field mapping by the model's new name",
			models:  blogModels("content"),
			renames: []Rename{renamePostToArticle, {App: "posts", Table: "article", Column: "body", To: "content"}},
			wants:   []string{"posts.article.body", "old name", "posts.post"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migrations, err := DiffModels(tt.models, mustReplay(t, blogHistory()...), tt.renames...)
			if !errors.Is(err, ErrInvalidRename) || migrations != nil {
				t.Fatalf("DiffModels = %#v, %v; want ErrInvalidRename and no migrations", migrations, err)
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// TestMigrationsReferencingARenamedModelRunAfterTheRename checks the
// ordering across apps: a pending migration adding a foreign key to the
// new table name must wait for the rename, even though its name sorts
// first. PostgreSQL rejects a REFERENCES clause naming a missing table.
func TestMigrationsReferencingARenamedModelRunAfterTheRename(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	history := blogHistory()
	if err := ApplyPending(ctx, sqlDB, dialect, history); err != nil {
		t.Fatalf("apply history: %v", err)
	}
	all := append(history,
		Migration{App: "posts", Name: "0004_rename", Reversible: true,
			Up: []Step{RenameTable{From: "post", To: "article"}}, Down: []Step{RenameTable{From: "article", To: "post"}}},
		Migration{App: "comments", Name: "0003_featured", Reversible: true,
			Up:   []Step{AddColumn{Table: "comment", Column: Column{Name: "featured_id", Type: "integer", References: "article"}}},
			Down: []Step{DropColumn{Table: "comment", Column: "featured_id"}}},
	)

	if err := ApplyPending(ctx, sqlDB, dialect, all); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	mustExec(t, sqlDB, `INSERT INTO article (body) VALUES ('hello')`)
	mustExec(t, sqlDB, `INSERT INTO comment (post_id, featured_id) VALUES (1, 1)`)
	mustFail(t, sqlDB, "featured_id references article", `INSERT INTO comment (post_id, featured_id) VALUES (1, 999)`)
}

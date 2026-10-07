package migrationtest_test

import (
	"testing"

	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// Comment references Post, which sorts after it, so its table can only be
// created once Post's exists.
type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
}

type Post struct {
	ID    int64 `tango:"pk"`
	Title string
}

func TestApplyCreatesATableForEachModel(t *testing.T) {
	registry := model.NewRegistry()
	for _, value := range []any{Comment{}, Post{}} {
		if err := registry.Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}

	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, registry.All())

	if _, err := sqlDB.Exec(`INSERT INTO post (id, title) VALUES (1, 'hello')`); err != nil {
		t.Fatalf("insert into post: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO comment (id, post_id) VALUES (1, 1)`); err != nil {
		t.Fatalf("insert into comment: %v", err)
	}
}

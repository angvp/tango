package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

type cascadeAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

type cascadePost struct {
	ID       int64 `tango:"pk"`
	AuthorID int64 `tango:"fk=cascadeAuthor"`
	Title    string
}

type cascadeComment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=cascadePost"`
	Body   string
}

type cascadeEmployee struct {
	ID        int64 `tango:"pk"`
	Name      string
	ManagerID int64 `tango:"fk=cascadeEmployee"`
}

func openCascadeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	statements := []string{
		`CREATE TABLE cascade_author (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`,
		`CREATE TABLE cascade_post (id INTEGER PRIMARY KEY AUTOINCREMENT, author_id INTEGER NOT NULL, title TEXT NOT NULL)`,
		`CREATE TABLE cascade_comment (id INTEGER PRIMARY KEY AUTOINCREMENT, post_id INTEGER NOT NULL, body TEXT NOT NULL)`,
		`CREATE TABLE cascade_employee (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, manager_id INTEGER)`,
	}
	for _, stmt := range statements {
		if _, err := sqlDB.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return sqlDB
}

func cascadeTestRegistry(t *testing.T) *model.Registry {
	t.Helper()
	registry := model.NewRegistry()
	for _, v := range []any{cascadeAuthor{}, cascadePost{}, cascadeComment{}, cascadeEmployee{}} {
		if err := registry.Register(v); err != nil {
			t.Fatalf("register %T: %v", v, err)
		}
	}
	return registry
}

func TestStoreDeleteWithoutUseModelsDoesNotCascade(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	registry := cascadeTestRegistry(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := NewStore(sqlDB, SQLite) // UseModels never called

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	if err := store.Delete(context.Background(), authorMeta, author.ID); err != nil {
		t.Fatalf("Delete author returned error: %v", err)
	}

	var gotPost cascadePost
	if err := store.Get(context.Background(), postMeta, post.ID, &gotPost); err != nil {
		t.Fatalf("post should still exist without UseModels, Get returned: %v", err)
	}
}

func TestStoreDeleteCascadesOneLevel(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	registry := cascadeTestRegistry(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	if err := store.Delete(context.Background(), authorMeta, author.ID); err != nil {
		t.Fatalf("Delete author returned error: %v", err)
	}

	var gotPost cascadePost
	err := store.Get(context.Background(), postMeta, post.ID, &gotPost)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("post should have been cascade-deleted, Get error = %v", err)
	}
}

func TestStoreDeleteCascadesMultiLevelChain(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	registry := cascadeTestRegistry(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")
	commentMeta, _ := registry.Get("cascadeComment")

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}
	comment := cascadeComment{PostID: post.ID, Body: "Nice post"}
	if err := store.Create(context.Background(), commentMeta, &comment); err != nil {
		t.Fatalf("Create comment: %v", err)
	}

	if err := store.Delete(context.Background(), authorMeta, author.ID); err != nil {
		t.Fatalf("Delete author returned error: %v", err)
	}

	var gotComment cascadeComment
	err := store.Get(context.Background(), commentMeta, comment.ID, &gotComment)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("comment should have been cascade-deleted through the chain, Get error = %v", err)
	}
}

func TestStoreDeleteCascadeHandlesSelfReferentialCycleWithoutHanging(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	registry := cascadeTestRegistry(t)
	employeeMeta, _ := registry.Get("cascadeEmployee")

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	manager := cascadeEmployee{Name: "Boss"}
	if err := store.Create(context.Background(), employeeMeta, &manager); err != nil {
		t.Fatalf("Create manager: %v", err)
	}
	report := cascadeEmployee{Name: "Report", ManagerID: manager.ID}
	if err := store.Create(context.Background(), employeeMeta, &report); err != nil {
		t.Fatalf("Create report: %v", err)
	}
	// Manager reports to their own report — a two-node cycle.
	manager.ManagerID = report.ID
	if err := store.Update(context.Background(), employeeMeta, &manager); err != nil {
		t.Fatalf("Update manager to create cycle: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.Delete(context.Background(), employeeMeta, manager.ID)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Delete returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Delete did not return — cascade likely looped on the circular foreign key reference")
	}

	var gotManager, gotReport cascadeEmployee
	if err := store.Get(context.Background(), employeeMeta, manager.ID, &gotManager); !errors.Is(err, ErrNotFound) {
		t.Fatalf("manager should have been deleted, Get error = %v", err)
	}
	if err := store.Get(context.Background(), employeeMeta, report.ID, &gotReport); !errors.Is(err, ErrNotFound) {
		t.Fatalf("report should have been deleted via the cycle, Get error = %v", err)
	}
}

func TestStoreDeleteCascadeFailureRollsBackCleanly(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	registry := cascadeTestRegistry(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	// Drop the dependent table out from under the cascade after rows exist,
	// so deleting its row mid-cascade fails and the whole transaction must
	// roll back — the author row must survive untouched.
	if _, err := sqlDB.ExecContext(context.Background(), "DROP TABLE cascade_post"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if err := store.Delete(context.Background(), authorMeta, author.ID); err == nil {
		t.Fatal("Delete returned nil error, want the failed cascade DELETE on the dropped table to surface")
	}

	var gotAuthor cascadeAuthor
	if err := store.Get(context.Background(), authorMeta, author.ID, &gotAuthor); err != nil {
		t.Fatalf("author should still exist after a rolled-back cascade failure, Get error = %v", err)
	}
}

func TestStoreDeleteRootNotFoundStillReturnsErrNotFoundWithModels(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	registry := cascadeTestRegistry(t)
	authorMeta, _ := registry.Get("cascadeAuthor")

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	err := store.Delete(context.Background(), authorMeta, int64(999))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

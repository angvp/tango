package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
)

func TestStoreCreateWithValidForeignKeySucceeds(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}

	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post with valid foreign key returned error: %v", err)
	}
}

func TestStoreCreateWithInvalidForeignKeyFails(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	postMeta, _ := registry.Get("cascadePost")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	post := cascadePost{AuthorID: 999, Title: "Hello"}
	err := store.Create(context.Background(), postMeta, &post)
	if !errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("Create error = %v, want it to wrap db.ErrInvalidForeignKey", err)
	}

	var count int
	if scanErr := sqlDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM cascade_post").Scan(&count); scanErr != nil {
		t.Fatalf("count posts: %v", scanErr)
	}
	if count != 0 {
		t.Fatalf("post count = %d, want 0 — Create should not have inserted a row on invalid foreign key", count)
	}
}

func TestStoreUpdateWithInvalidForeignKeyFails(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	post.AuthorID = 999
	err := store.Update(context.Background(), postMeta, &post)
	if !errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("Update error = %v, want it to wrap db.ErrInvalidForeignKey", err)
	}

	var gotPost cascadePost
	if getErr := store.Get(context.Background(), postMeta, post.ID, &gotPost); getErr != nil {
		t.Fatalf("Get post after failed update: %v", getErr)
	}
	if gotPost.AuthorID != author.ID {
		t.Fatalf("post AuthorID = %d, want it unchanged at %d — Update should not have applied on invalid foreign key", gotPost.AuthorID, author.ID)
	}
}

func TestStoreCreateSkipsForeignKeyValidationForZeroValue(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	employeeMeta, _ := registry.Get("cascadeEmployee")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	// ManagerID left at its zero value: treated as "unset," not a reference
	// to primary key 0.
	employee := cascadeEmployee{Name: "Solo contributor"}
	if err := store.Create(context.Background(), employeeMeta, &employee); err != nil {
		t.Fatalf("Create with zero-value foreign key returned error: %v", err)
	}
}

func TestStoreForeignKeyValidationInertWithoutUseModels(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	postMeta, _ := registry.Get("cascadePost")

	store := db.NewStore(sqlDB, dialect) // UseModels never called

	// The Store doesn't check the reference, so the error comes from the
	// database's own REFERENCES constraint and is never ErrInvalidForeignKey.
	post := cascadePost{AuthorID: 999, Title: "Hello"}
	err := store.Create(context.Background(), postMeta, &post)
	if err == nil {
		t.Fatal("Create without UseModels returned nil error, want the database's foreign key constraint to refuse it")
	}
	if errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("Create without UseModels error = %v, want the driver's constraint error, not ErrInvalidForeignKey", err)
	}
}

func TestStoreCreateValidatesMultipleForeignKeyFieldsIndependently(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")
	commentMeta, _ := registry.Get("cascadeComment")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(context.Background(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(context.Background(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	valid := cascadeComment{PostID: post.ID, Body: "Nice post"}
	if err := store.Create(context.Background(), commentMeta, &valid); err != nil {
		t.Fatalf("Create comment with valid foreign key returned error: %v", err)
	}

	invalid := cascadeComment{PostID: 999, Body: "Orphaned"}
	if err := store.Create(context.Background(), commentMeta, &invalid); !errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("Create comment error = %v, want it to wrap db.ErrInvalidForeignKey", err)
	}
}

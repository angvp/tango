package tango

import (
	"errors"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/testdb"
)

func TestRegistrySetStoreAndStoreRoundTrip(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	registry := NewRegistry()
	store := db.NewStore(sqlDB, dialect)
	registry.SetStore(store)

	got, ok := registry.Store()
	if !ok {
		t.Fatal("Store() returned false after SetStore")
	}
	if got != store {
		t.Fatal("Store() did not return the exact store passed to SetStore")
	}
}

func TestRegistryStoreUnsetReturnsFalse(t *testing.T) {
	registry := NewRegistry()

	_, ok := registry.Store()
	if ok {
		t.Fatal("Store() returned true before SetStore was ever called")
	}
}

type registrySetStoreCascadeAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

type registrySetStoreCascadePost struct {
	ID       int64 `tango:"pk"`
	AuthorID int64 `tango:"fk=registrySetStoreCascadeAuthor"`
}

func TestRegistrySetStoreWiresModelsForCascadeDelete(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)

	registry := NewRegistry()
	if err := registry.Models().Register(registrySetStoreCascadeAuthor{}); err != nil {
		t.Fatalf("register author: %v", err)
	}
	if err := registry.Models().Register(registrySetStoreCascadePost{}); err != nil {
		t.Fatalf("register post: %v", err)
	}
	migrationtest.Apply(t, sqlDB, dialect, registry.Models().All())

	store := db.NewStore(sqlDB, dialect)
	registry.SetStore(store) // must wire Models() into store for cascade to work

	authorMeta, _ := registry.Models().Get("registrySetStoreCascadeAuthor")
	postMeta, _ := registry.Models().Get("registrySetStoreCascadePost")

	author := registrySetStoreCascadeAuthor{Name: "Jane"}
	if err := store.Create(t.Context(), authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := registrySetStoreCascadePost{AuthorID: author.ID}
	if err := store.Create(t.Context(), postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	if err := store.Delete(t.Context(), authorMeta, author.ID); err != nil {
		t.Fatalf("Delete author returned error: %v", err)
	}

	var gotPost registrySetStoreCascadePost
	if err := store.Get(t.Context(), postMeta, post.ID, &gotPost); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("post should have been cascade-deleted via SetStore's automatic UseModels wiring, Get error = %v", err)
	}
}

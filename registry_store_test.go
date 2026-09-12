package tango

import (
	"database/sql"
	"testing"

	"github.com/angvp/tango/db"
	_ "modernc.org/sqlite"
)

func TestRegistrySetStoreAndStoreRoundTrip(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	registry := NewRegistry()
	store := db.NewStore(sqlDB, db.SQLite)
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

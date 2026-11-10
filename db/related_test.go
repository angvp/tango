package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

type relatedAccount struct {
	ID   int64 `tango:"pk"`
	Name string
}

type relatedPost struct {
	ID        int64 `tango:"pk"`
	Title     string
	AccountID int64 `tango:"fk=relatedAccount"`
}

func createRelatedAccount(t *testing.T, store *db.Store, meta model.ModelMeta, name string) relatedAccount {
	t.Helper()
	account := relatedAccount{Name: name}
	if err := store.Create(context.Background(), meta, &account); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return account
}

func TestRelatedLoadsRowsByPrimaryKey(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	store := db.NewStore(sqlDB, dialect)

	first := createRelatedAccount(t, store, accounts, "first")
	second := createRelatedAccount(t, store, accounts, "second")
	posts := []relatedPost{
		{Title: "a", AccountID: first.ID},
		{Title: "b", AccountID: first.ID},
		{Title: "c", AccountID: second.ID},
		{Title: "unset", AccountID: 0},
		{Title: "dangling", AccountID: second.ID + 100},
	}

	dest := map[int64]relatedAccount{}
	if err := store.Related(context.Background(), posts, "AccountID", accounts, dest); err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(dest) != 2 {
		t.Fatalf("dest holds %d entries, want 2 (the set keys that have rows): %v", len(dest), dest)
	}
	if dest[first.ID].Name != "first" || dest[second.ID].Name != "second" {
		t.Errorf("dest = %v, want first and second by primary key", dest)
	}
}

func TestRelatedLoadsKeysAcrossChunks(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	const count = db.MaxInValues + 1
	ids := make([]int64, 0, count)
	err := store.InTx(ctx, func(tx *db.Store) error {
		for i := 0; i < count; i++ {
			account := relatedAccount{Name: "n"}
			if err := tx.Create(ctx, accounts, &account); err != nil {
				return err
			}
			ids = append(ids, account.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed accounts: %v", err)
	}

	posts := make([]relatedPost, len(ids))
	for i, id := range ids {
		posts[i] = relatedPost{AccountID: id}
	}
	dest := map[int64]relatedAccount{}
	if err := store.Related(ctx, posts, "AccountID", accounts, dest); err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(dest) != count {
		t.Errorf("dest holds %d entries, want %d (one per key across two chunks)", len(dest), count)
	}
}

func TestRelatedReadsInsideTransaction(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()
	rollback := errors.New("roll back")

	var posts []relatedPost
	err := store.InTx(ctx, func(tx *db.Store) error {
		account := createRelatedAccount(t, tx, accounts, "uncommitted")
		posts = []relatedPost{{AccountID: account.ID}}
		dest := map[int64]relatedAccount{}
		if err := tx.Related(ctx, posts, "AccountID", accounts, dest); err != nil {
			return err
		}
		if dest[account.ID].Name != "uncommitted" {
			t.Errorf("inside InTx dest = %v, want the uncommitted row", dest)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx error = %v, want the rollback sentinel", err)
	}

	dest := map[int64]relatedAccount{}
	if err := store.Related(ctx, posts, "AccountID", accounts, dest); err != nil {
		t.Fatalf("Related after rollback: %v", err)
	}
	if len(dest) != 0 {
		t.Errorf("after rollback dest = %v, want no entries", dest)
	}
}

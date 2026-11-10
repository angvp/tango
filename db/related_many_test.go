package db_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/db"
)

func TestRelatedManyGroupsChildrenByParent(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	posts := metaFor(t, registry, relatedPost{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	first := createRelatedAccount(t, store, accounts, "first")
	second := createRelatedAccount(t, store, accounts, "second")
	empty := createRelatedAccount(t, store, accounts, "empty")
	for _, post := range []relatedPost{
		{Title: "a1", AccountID: first.ID},
		{Title: "a2", AccountID: first.ID},
		{Title: "b1", AccountID: second.ID},
		{Title: "unset", AccountID: 0},
	} {
		if err := store.Create(ctx, posts, &post); err != nil {
			t.Fatalf("create %s: %v", post.Title, err)
		}
	}

	parents := []relatedAccount{first, second, empty}
	dest := map[int64][]relatedPost{}
	if err := store.RelatedMany(ctx, parents, "ID", posts, "AccountID", db.Query{}, dest); err != nil {
		t.Fatalf("RelatedMany: %v", err)
	}
	if len(dest) != 2 {
		t.Fatalf("dest holds %d keys, want 2 (the parents with children): %v", len(dest), dest)
	}
	titles := func(children []relatedPost) []string {
		out := make([]string, len(children))
		for i, child := range children {
			out[i] = child.Title
		}
		return out
	}
	if got := titles(dest[first.ID]); len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Errorf("first's children = %v, want [a1 a2] in primary-key order", got)
	}
	if got := titles(dest[second.ID]); len(got) != 1 || got[0] != "b1" {
		t.Errorf("second's children = %v, want [b1]", got)
	}
}

func TestRelatedManyAppliesQueryConditionsAndOrder(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	posts := metaFor(t, registry, relatedPost{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	owner := createRelatedAccount(t, store, accounts, "owner")
	for _, title := range []string{"keep-1", "drop", "keep-2"} {
		post := relatedPost{Title: title, AccountID: owner.ID}
		if err := store.Create(ctx, posts, &post); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
	}
	parents := []relatedAccount{owner}

	titlesFor := func(query db.Query) []string {
		t.Helper()
		dest := map[int64][]relatedPost{}
		if err := store.RelatedMany(ctx, parents, "ID", posts, "AccountID", query, dest); err != nil {
			t.Fatalf("RelatedMany: %v", err)
		}
		var out []string
		for _, child := range dest[owner.ID] {
			out = append(out, child.Title)
		}
		return out
	}

	if got := titlesFor(db.Query{}); len(got) != 3 {
		t.Fatalf("default query returned %v, want all three children", got)
	}
	got := titlesFor(db.Query{
		Where:   []db.Condition{{Field: "Title", Op: db.OpLike, Value: "keep%"}},
		OrderBy: []string{"-Title"},
	})
	if len(got) != 2 || got[0] != "keep-2" || got[1] != "keep-1" {
		t.Errorf("filtered and ordered children = %v, want [keep-2 keep-1]", got)
	}
}

func TestRelatedManyLoadsKeysAcrossChunks(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	posts := metaFor(t, registry, relatedPost{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	const count = db.MaxInValues + 1
	parents := make([]relatedAccount, 0, count)
	err := store.InTx(ctx, func(tx *db.Store) error {
		for i := 0; i < count; i++ {
			account := relatedAccount{Name: "n"}
			if err := tx.Create(ctx, accounts, &account); err != nil {
				return err
			}
			parents = append(parents, account)
			for j := 0; j < 2; j++ {
				post := relatedPost{Title: "p", AccountID: account.ID}
				if err := tx.Create(ctx, posts, &post); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	dest := map[int64][]relatedPost{}
	if err := store.RelatedMany(ctx, parents, "ID", posts, "AccountID", db.Query{}, dest); err != nil {
		t.Fatalf("RelatedMany: %v", err)
	}
	if len(dest) != count {
		t.Fatalf("dest holds %d keys, want %d", len(dest), count)
	}
	for _, parent := range parents {
		children := dest[parent.ID]
		if len(children) != 2 || children[0].ID >= children[1].ID {
			t.Fatalf("parent %d children = %v, want two in ascending primary-key order", parent.ID, children)
		}
		for _, child := range children {
			if child.AccountID != parent.ID {
				t.Fatalf("child %d under parent %d has AccountID %d", child.ID, parent.ID, child.AccountID)
			}
		}
	}
}

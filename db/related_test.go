package db_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/querycount"
	"github.com/angvp/tango/model"
)

// errInjectedChunk is the error a fault-injected later chunk returns.
var errInjectedChunk = errors.New("injected failure on a later chunk")

// seedRelatedAccounts creates count accounts in one transaction and returns
// them with their generated primary keys.
func seedRelatedAccounts(t *testing.T, store *db.Store, meta model.ModelMeta, count int) []relatedAccount {
	t.Helper()
	ctx := context.Background()
	accounts := make([]relatedAccount, 0, count)
	err := store.InTx(ctx, func(tx *db.Store) error {
		for i := 0; i < count; i++ {
			account := relatedAccount{Name: "n"}
			if err := tx.Create(ctx, meta, &account); err != nil {
				return err
			}
			accounts = append(accounts, account)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed accounts: %v", err)
	}
	return accounts
}

func TestRelatedLeavesDestUntouchedWhenALaterChunkFails(t *testing.T) {
	counter := querycount.Attach(t)
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	seeded := seedRelatedAccounts(t, store, accounts, db.MaxInValues+1)
	posts := make([]relatedPost, len(seeded))
	for i, account := range seeded {
		posts[i] = relatedPost{AccountID: account.ID}
	}
	const fragment = "related_account"

	// Control: the same read succeeds and sends two chunked statements.
	counter.Reset()
	if err := store.Related(ctx, posts, "AccountID", accounts, map[int64]relatedAccount{}); err != nil {
		t.Fatalf("control Related: %v", err)
	}
	if n := counter.Matching(fragment); n != 2 {
		t.Fatalf("control sent %d statements for %q, want 2 chunks", n, fragment)
	}

	// The second chunk fails; the first chunk has already loaded a row for
	// seeded[0], which must not reach dest.
	counter.Reset()
	counter.FailNthMatching(fragment, 2, errInjectedChunk)
	dest := map[int64]relatedAccount{seeded[0].ID: {ID: seeded[0].ID, Name: "old"}}
	want := map[int64]relatedAccount{seeded[0].ID: {ID: seeded[0].ID, Name: "old"}}

	err := store.Related(ctx, posts, "AccountID", accounts, dest)
	if !errors.Is(err, errInjectedChunk) {
		t.Fatalf("Related error = %v, want the injected chunk failure", err)
	}
	if n := counter.Matching(fragment); n != 1 {
		t.Fatalf("recorded %d statements before the failure, want 1 (the first chunk)", n)
	}
	if !reflect.DeepEqual(dest, want) {
		t.Errorf("dest changed on a later-chunk failure: got %v, want %v", dest, want)
	}
}

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

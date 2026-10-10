package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
)

func intxStore(t *testing.T) *db.Store {
	t.Helper()
	sqlDB, dialect, registry := openCascadeTestDB(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	return store
}

func authorCount(t *testing.T, store *db.Store) int {
	t.Helper()
	registry := registerModels(t, cascadeAuthor{})
	meta := metaFor(t, registry, cascadeAuthor{})
	n, err := store.Count(context.Background(), meta, db.Query{})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestInTxCommitsOnNilAndRollsBackOnAnError(t *testing.T) {
	store := intxStore(t)
	registry := registerModels(t, cascadeAuthor{})
	meta := metaFor(t, registry, cascadeAuthor{})
	ctx := context.Background()

	if err := store.InTx(ctx, func(tx *db.Store) error {
		return tx.Create(ctx, meta, &cascadeAuthor{Name: "kept"})
	}); err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if got := authorCount(t, store); got != 1 {
		t.Fatalf("authors after a nil return = %d, want 1", got)
	}

	boom := errors.New("boom")
	err := store.InTx(ctx, func(tx *db.Store) error {
		if err := tx.Create(ctx, meta, &cascadeAuthor{Name: "dropped"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx error = %v, want the callback's error", err)
	}
	if got := authorCount(t, store); got != 1 {
		t.Fatalf("authors after an error = %d, want the write rolled back (1)", got)
	}
}

func TestInTxRollsBackAndRepanicsWithTheSameValue(t *testing.T) {
	store := intxStore(t)
	registry := registerModels(t, cascadeAuthor{})
	meta := metaFor(t, registry, cascadeAuthor{})
	ctx := context.Background()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = store.InTx(ctx, func(tx *db.Store) error {
			if err := tx.Create(ctx, meta, &cascadeAuthor{Name: "dropped"}); err != nil {
				return err
			}
			panic("programmer error")
		})
	}()
	if recovered != "programmer error" {
		t.Fatalf("recovered = %v, want the original panic value re-raised", recovered)
	}
	if got := authorCount(t, store); got != 0 {
		t.Fatalf("authors after a panic = %d, want 0", got)
	}
}

func TestNestedInTxJoinsTheOuterTransaction(t *testing.T) {
	store := intxStore(t)
	registry := registerModels(t, cascadeAuthor{})
	meta := metaFor(t, registry, cascadeAuthor{})
	ctx := context.Background()

	boom := errors.New("outer fails")
	err := store.InTx(ctx, func(tx *db.Store) error {
		if err := tx.InTx(ctx, func(inner *db.Store) error {
			return inner.Create(ctx, meta, &cascadeAuthor{Name: "inner"})
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v", err)
	}
	if got := authorCount(t, store); got != 0 {
		t.Fatalf("the inner write survived the outer rollback: %d rows", got)
	}
}

func TestASwallowedInnerErrorCanStillCommit(t *testing.T) {
	store := intxStore(t)
	registry := registerModels(t, cascadeAuthor{})
	meta := metaFor(t, registry, cascadeAuthor{})
	ctx := context.Background()

	if err := store.InTx(ctx, func(tx *db.Store) error {
		if err := tx.Create(ctx, meta, &cascadeAuthor{Name: "outer"}); err != nil {
			return err
		}
		_ = tx.InTx(ctx, func(inner *db.Store) error {
			if err := inner.Create(ctx, meta, &cascadeAuthor{Name: "inner"}); err != nil {
				return err
			}
			return errors.New("inner failed, swallowed by the outer callback")
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := authorCount(t, store); got != 2 {
		t.Fatalf("authors = %d, want both writes committed (flat join, no savepoints)", got)
	}
}

func TestExplicitIDInsertAndCascadingDeleteRunOnTheOuterTransaction(t *testing.T) {
	store := intxStore(t)
	sqlRegistry := registerModels(t, cascadeAuthor{}, cascadePost{})
	authors := metaFor(t, sqlRegistry, cascadeAuthor{})
	posts := metaFor(t, sqlRegistry, cascadePost{})
	ctx := context.Background()

	if err := store.InTx(ctx, func(tx *db.Store) error {
		author := cascadeAuthor{ID: 500, Name: "explicit"}
		if err := tx.Create(ctx, authors, &author); err != nil {
			return err
		}
		if err := tx.Create(ctx, posts, &cascadePost{AuthorID: 500, Title: "t"}); err != nil {
			return err
		}
		return tx.Delete(ctx, authors, int64(500)) // cascades to the post, in the same transaction
	}); err != nil {
		t.Fatalf("explicit-ID insert and cascading delete inside InTx: %v", err)
	}
	if got := authorCount(t, store); got != 0 {
		t.Fatalf("authors = %d, want 0", got)
	}

	// A later insert without an ID still draws a fresh key past the explicit one.
	next := cascadeAuthor{Name: "after"}
	if err := store.Create(ctx, authors, &next); err != nil || next.ID <= 0 {
		t.Fatalf("Create after the transaction: id %d, %v", next.ID, err)
	}
}

func TestInTxCarriesStoreValidationAndRollsBackEarlierWrites(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, Headline{})
	store := db.NewStore(sqlDB, dialect)
	meta, _ := registry.Get("Headline")
	ctx := context.Background()

	err := store.InTx(ctx, func(tx *db.Store) error {
		if err := tx.Create(ctx, meta, &Headline{Title: "fits"}); err != nil {
			return err
		}
		return tx.Create(ctx, meta, &Headline{Title: strings.Repeat("x", 11)})
	})
	if !errors.Is(err, db.ErrValueTooLong) {
		t.Fatalf("InTx error = %v, want ErrValueTooLong", err)
	}
	if got := countHeadlines(t, store, meta); got != 0 {
		t.Fatalf("headlines = %d, want the earlier write rolled back", got)
	}
}

func TestInTxReportsABeginFailure(t *testing.T) {
	sqlDB, dialect, _ := openTables(t, Headline{})
	store := db.NewStore(sqlDB, dialect)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := store.InTx(ctx, func(*db.Store) error { called = true; return nil }); err == nil || called {
		t.Fatalf("InTx on a canceled context: err=%v called=%v, want an error and no callback", err, called)
	}
}

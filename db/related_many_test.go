package db_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/querycount"
)

func TestRelatedManyAppliesAnyAndFilter(t *testing.T) {
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

	// Control: with no narrowing every child comes back.
	if got := titlesFor(db.Query{}); len(got) != 3 {
		t.Fatalf("default query returned %v, want all three children", got)
	}
	anyTitles := titlesFor(db.Query{Any: []db.Condition{
		{Field: "Title", Op: db.OpEq, Value: "drop"},
		{Field: "Title", Op: db.OpEq, Value: "keep-2"},
	}})
	if want := []string{"drop", "keep-2"}; !slices.Equal(anyTitles, want) {
		t.Errorf("Any narrowed children to %v, want %v", anyTitles, want)
	}
	filter := db.Cond("Title", db.OpEq, "drop")
	filterTitles := titlesFor(db.Query{Filter: &filter})
	if want := []string{"drop"}; !slices.Equal(filterTitles, want) {
		t.Errorf("Filter narrowed children to %v, want %v", filterTitles, want)
	}
}

func TestRelatedManyLeavesDestUntouchedWhenALaterChunkFails(t *testing.T) {
	counter := querycount.Attach(t)
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	posts := metaFor(t, registry, relatedPost{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	seeded := seedRelatedAccounts(t, store, accounts, db.MaxInValues+1)
	first := relatedPost{Title: "first", AccountID: seeded[0].ID}
	if err := store.Create(ctx, posts, &first); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parents := seeded
	const fragment = "related_post"

	// Control: the same read succeeds and sends two chunked statements.
	counter.Reset()
	if err := store.RelatedMany(ctx, parents, "ID", posts, "AccountID", db.Query{}, map[int64][]relatedPost{}); err != nil {
		t.Fatalf("control RelatedMany: %v", err)
	}
	if n := counter.Matching(fragment); n != 2 {
		t.Fatalf("control sent %d statements for %q, want 2 chunks", n, fragment)
	}

	// The second chunk fails; the first chunk has already loaded seeded[0]'s
	// child, which must not reach dest.
	counter.Reset()
	counter.FailNthMatching(fragment, 2, errInjectedChunk)
	old := []relatedPost{{Title: "old"}}
	dest := map[int64][]relatedPost{seeded[0].ID: old}
	want := map[int64][]relatedPost{seeded[0].ID: old}

	err := store.RelatedMany(ctx, parents, "ID", posts, "AccountID", db.Query{}, dest)
	if !errors.Is(err, errInjectedChunk) {
		t.Fatalf("RelatedMany error = %v, want the injected chunk failure", err)
	}
	if n := counter.Matching(fragment); n != 1 {
		t.Fatalf("recorded %d statements before the failure, want 1 (the first chunk)", n)
	}
	if !reflect.DeepEqual(dest, want) {
		t.Errorf("dest changed on a later-chunk failure: got %v, want %v", dest, want)
	}
}

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

func TestRelatedManyRefusesPreloadBeforeAnySQL(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	post := createPreloadPost(t, store, posts, "p", 0)
	createPreloadComment(t, store, comments, post.ID, "c")
	parents := []preloadPost{post}
	const fragment = "preload_comment"

	// Control: the same read without Preload sends a statement.
	counter.Reset()
	if err := store.RelatedMany(ctx, parents, "ID", comments, "PostID", db.Query{}, map[int64][]preloadComment{}); err != nil {
		t.Fatalf("control RelatedMany: %v", err)
	}
	if n := counter.Matching(fragment); n != 1 {
		t.Fatalf("control sent %d statements for %q, want 1", n, fragment)
	}

	// Preload names a valid relation of preloadComment, so the refusal must
	// come from RelatedMany itself, not from an unknown relation.
	counter.Reset()
	dest := map[int64][]preloadComment{}
	err := store.RelatedMany(ctx, parents, "ID", comments, "PostID", db.Query{Preload: []string{"Post"}}, dest)
	var queryErr *db.QueryError
	if !errors.As(err, &queryErr) {
		t.Fatalf("RelatedMany with Preload error = %v, want a *db.QueryError", err)
	}
	if n := len(counter.Statements()); n != 0 {
		t.Fatalf("Preload was refused after sending %d statements, want none", n)
	}
	if len(dest) != 0 {
		t.Errorf("dest changed on refusal: %v", dest)
	}
}

func TestRelatedManyAcceptsSliceOrPointerToSlice(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, relatedAccount{}, relatedPost{})
	accounts := metaFor(t, registry, relatedAccount{})
	posts := metaFor(t, registry, relatedPost{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	owner := createRelatedAccount(t, store, accounts, "owner")
	if err := store.Create(ctx, posts, &relatedPost{Title: "p", AccountID: owner.ID}); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parents := []relatedAccount{owner}

	fromSlice := map[int64][]relatedPost{}
	if err := store.RelatedMany(ctx, parents, "ID", posts, "AccountID", db.Query{}, fromSlice); err != nil {
		t.Fatalf("RelatedMany with a slice: %v", err)
	}
	if len(fromSlice[owner.ID]) != 1 {
		t.Fatalf("slice form loaded %v, want the one child", fromSlice)
	}

	fromPointer := map[int64][]relatedPost{}
	if err := store.RelatedMany(ctx, &parents, "ID", posts, "AccountID", db.Query{}, fromPointer); err != nil {
		t.Fatalf("RelatedMany with a pointer to a slice: %v", err)
	}
	if !reflect.DeepEqual(fromPointer, fromSlice) {
		t.Errorf("pointer form loaded %v, want %v", fromPointer, fromSlice)
	}
}

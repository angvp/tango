package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/querycount"
	"github.com/angvp/tango/model"
)

func createPreloadComment(t *testing.T, store *db.Store, meta model.ModelMeta, postID int64, body string) preloadComment {
	t.Helper()
	comment := preloadComment{PostID: postID, Body: body}
	if err := store.Create(context.Background(), meta, &comment); err != nil {
		t.Fatalf("create comment %s: %v", body, err)
	}
	return comment
}

func TestListPreloadHasManyFillsChildrenInPrimaryKeyOrder(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})

	busy := createPreloadPost(t, store, posts, "busy", 0)
	createPreloadPost(t, store, posts, "quiet", 0)
	single := createPreloadPost(t, store, posts, "single", 0)
	first := createPreloadComment(t, store, comments, busy.ID, "first")
	second := createPreloadComment(t, store, comments, busy.ID, "second")
	only := createPreloadComment(t, store, comments, single.ID, "only")
	counter.Reset()

	// Control: without Preload no children are read or set.
	var plain []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}}, &plain); err != nil {
		t.Fatalf("List without Preload: %v", err)
	}
	if plain[0].Comments != nil || counter.Matching("preload_comment") != 0 {
		t.Fatalf("control loaded comments: %+v", plain[0].Comments)
	}

	counter.Reset()
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments"}}, &got); err != nil {
		t.Fatalf("List with Preload: %v", err)
	}
	if n := len(counter.Statements()); n != 2 {
		t.Fatalf("sent %d statements, want 2: the rows and one child read", n)
	}
	if n := counter.Matching("preload_comment"); n != 1 {
		t.Fatalf("child read sent %d times, want 1", n)
	}

	ids := func(children []preloadComment) []int64 {
		out := make([]int64, len(children))
		for i, child := range children {
			out[i] = child.ID
		}
		return out
	}
	if want := []int64{first.ID, second.ID}; !reflect.DeepEqual(ids(got[0].Comments), want) {
		t.Errorf("busy children = %v, want %v in primary-key order", ids(got[0].Comments), want)
	}
	if got[1].Comments == nil || len(got[1].Comments) != 0 {
		t.Errorf("quiet children = %v, want an empty non-nil slice", got[1].Comments)
	}
	if want := []int64{only.ID}; !reflect.DeepEqual(ids(got[2].Comments), want) {
		t.Errorf("single children = %v, want %v", ids(got[2].Comments), want)
	}
	if got[0].Account != nil {
		t.Errorf("Account = %+v without being named, want nil", got[0].Account)
	}
	if got[0].Comments[0].Post != nil {
		t.Errorf("loaded child carries a parent %+v; children must not point back", got[0].Comments[0].Post)
	}

	// First reads the same relation for one row.
	var one preloadPost
	if err := store.First(ctx, posts, db.Query{Where: []db.Condition{{Field: "Title", Op: db.OpEq, Value: "quiet"}}, Preload: []string{"Comments"}}, &one); err != nil {
		t.Fatalf("First with Preload: %v", err)
	}
	if one.Comments == nil || len(one.Comments) != 0 {
		t.Errorf("First children for a parent with none = %v, want an empty non-nil slice", one.Comments)
	}
}

func TestListPreloadTwoRelationsCostOneReadEach(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})

	alpha := createPreloadAccount(t, store, accounts, "alpha")
	beta := createPreloadAccount(t, store, accounts, "beta")
	one := createPreloadPost(t, store, posts, "one", alpha.ID)
	two := createPreloadPost(t, store, posts, "two", beta.ID)
	createPreloadComment(t, store, comments, one.ID, "c1")
	createPreloadComment(t, store, comments, two.ID, "c2")
	counter.Reset()

	var got []preloadPost
	query := db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account", "Comments"}}
	if err := store.List(ctx, posts, query, &got); err != nil {
		t.Fatalf("List with two relations: %v", err)
	}
	if n := len(counter.Statements()); n != 3 {
		t.Fatalf("sent %d statements, want 3: the rows, one account read, one child read", n)
	}
	if counter.Matching("preload_account") != 1 || counter.Matching("preload_comment") != 1 {
		t.Fatalf("relation reads = %v, want one per relation", counter.Statements())
	}
	if got[0].Account == nil || got[0].Account.Name != "alpha" || len(got[0].Comments) != 1 || got[0].Comments[0].Body != "c1" {
		t.Errorf("row one = %+v, want alpha and c1", got[0])
	}
}

func TestListPreloadHasManyChunksParentsAndKeepsChildOrder(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})

	const count = db.MaxInValues + 1
	postIDs := make([]int64, 0, count)
	err := store.InTx(ctx, func(tx *db.Store) error {
		for i := 0; i < count; i++ {
			post := preloadPost{Title: "p"}
			if err := tx.Create(ctx, posts, &post); err != nil {
				return err
			}
			postIDs = append(postIDs, post.ID)
			for _, body := range []string{"first", "second"} {
				comment := preloadComment{PostID: post.ID, Body: body}
				if err := tx.Create(ctx, comments, &comment); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Control: without Preload the children are never read.
	counter.Reset()
	var plain []preloadPost
	if err := store.List(ctx, posts, db.Query{Limit: 1, OrderBy: []string{"ID"}}, &plain); err != nil {
		t.Fatalf("control List: %v", err)
	}
	if counter.Matching("preload_comment") != 0 {
		t.Fatalf("control read children without Preload")
	}

	counter.Reset()
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments"}}, &got); err != nil {
		t.Fatalf("List with Preload: %v", err)
	}
	if n := len(counter.Statements()); n != 3 {
		t.Fatalf("sent %d statements, want 3: the rows and two child reads for %d parents", n, count)
	}
	if n := counter.Matching("preload_comment"); n != 2 {
		t.Fatalf("child reads = %d, want 2", n)
	}
	if len(got) != count {
		t.Fatalf("List returned %d parents, want %d", len(got), count)
	}
	for i, post := range got {
		if post.ID != postIDs[i] || len(post.Comments) != 2 || post.Comments[0].ID >= post.Comments[1].ID {
			t.Fatalf("parent %d (id %d): children %+v, want two in ascending primary-key order", i, post.ID, post.Comments)
		}
	}
}

func TestListPreloadHasManyLeavesDestUntouchedWhenReadFails(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})

	alpha := createPreloadAccount(t, store, accounts, "alpha")
	post := createPreloadPost(t, store, posts, "p", alpha.ID)
	createPreloadComment(t, store, comments, post.ID, "c")
	counter.Reset()

	// Control: both relations load before any failure is armed.
	var control []preloadPost
	if err := store.List(ctx, posts, db.Query{Preload: []string{"Account", "Comments"}}, &control); err != nil {
		t.Fatalf("control List: %v", err)
	}
	if len(control) != 1 || control[0].Account == nil || len(control[0].Comments) != 1 {
		t.Fatalf("control did not load both relations: %+v", control)
	}

	// Account loads first; the child read then fails. Neither reaches dest.
	counter.FailMatching("preload_comment", errPreloadRead)
	sentinel := preloadPost{ID: 9, Title: "keep"}
	rows := []preloadPost{sentinel}
	err := store.List(ctx, posts, db.Query{Preload: []string{"Account", "Comments"}}, &rows)
	if !errors.Is(err, errPreloadRead) {
		t.Fatalf("List error = %v, want the injected read failure", err)
	}
	if !reflect.DeepEqual(rows, []preloadPost{sentinel}) {
		t.Errorf("List changed dest on a child read failure: %+v", rows)
	}
}

func TestPreloadHasManySQLGoldens(t *testing.T) {
	registry := registerModels(t, preloadAccount{}, preloadPost{}, preloadComment{})
	posts := metaFor(t, registry, preloadPost{})
	ctx := context.Background()

	for _, tc := range relatedDialects() {
		t.Run(tc.name, func(t *testing.T) {
			// Without rows the relation reads are never sent, so the parent
			// statement alone must be byte-identical with and without Preload.
			plain := &recorder{}
			plainStore := db.NewStore(sql.OpenDB(plain), tc.dialect)
			plainStore.UseModels(registry)
			var rows []preloadPost
			if err := plainStore.List(ctx, posts, db.Query{OrderBy: []string{"ID"}}, &rows); err != nil {
				t.Fatal(err)
			}

			preloaded := &recorder{}
			preloadStore := db.NewStore(sql.OpenDB(preloaded), tc.dialect)
			preloadStore.UseModels(registry)
			if err := preloadStore.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account", "Comments"}}, &rows); err != nil {
				t.Fatal(err)
			}

			want := plain.statements()
			got := preloaded.statements()
			if len(want) != 1 || len(got) != 1 {
				t.Fatalf("statements: plain %d, preload %d; want 1 each (no rows, so no relation read)", len(want), len(got))
			}
			if got[0] != want[0] {
				t.Errorf("Preload changed the parent statement:\n got  %+v\n want %+v", got[0], want[0])
			}
		})
	}
}

func TestListPreloadHasManyOneLevelOpensNoTransaction(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	post := preloadPost{Title: "p"}
	if err := store.Create(ctx, posts, &post); err != nil {
		t.Fatalf("create post: %v", err)
	}
	createPreloadComment(t, store, comments, post.ID, "c1")
	counter.Reset()

	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{Preload: []string{"Comments"}}, &got); err != nil {
		t.Fatalf("List with Preload: %v", err)
	}
	if n := len(counter.Statements()); n != 2 {
		t.Fatalf("sent %d statements, want 2", n)
	}
	if kinds := transactionKinds(counter.Events()); len(kinds) != 0 {
		t.Fatalf("one-level Preload recorded transaction events %v, want none", kinds)
	}

	// Known positive: the same counter records the transaction InTx opens.
	counter.Reset()
	err := store.InTx(ctx, func(tx *db.Store) error {
		return tx.List(ctx, posts, db.Query{Preload: []string{"Comments"}}, &got)
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if kinds := transactionKinds(counter.Events()); len(kinds) != 2 || kinds[0] != querycount.KindBegin || kinds[1] != querycount.KindCommit {
		t.Fatalf("InTx recorded %v, want begin then commit", kinds)
	}
}

func TestListPreloadHasManySingleChunkAtThousandParents(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})

	const count = db.MaxInValues
	postIDs := make([]int64, 0, count)
	err := store.InTx(ctx, func(tx *db.Store) error {
		for i := 0; i < count; i++ {
			post := preloadPost{Title: "p"}
			if err := tx.Create(ctx, posts, &post); err != nil {
				return err
			}
			postIDs = append(postIDs, post.ID)
			comment := preloadComment{PostID: post.ID, Body: "only"}
			if err := tx.Create(ctx, comments, &comment); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	counter.Reset()
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments"}}, &got); err != nil {
		t.Fatalf("List with Preload: %v", err)
	}
	if n := len(counter.Statements()); n != 2 {
		t.Fatalf("sent %d statements, want 2: the rows and one child read for %d parents", n, count)
	}
	if n := counter.Matching("preload_comment"); n != 1 {
		t.Fatalf("child reads = %d, want 1", n)
	}
	if len(got) != count {
		t.Fatalf("List returned %d parents, want %d", len(got), count)
	}
	for i, post := range got {
		if post.ID != postIDs[i] || len(post.Comments) != 1 || post.Comments[0].PostID != post.ID {
			t.Fatalf("parent %d (id %d): children %+v, want its one comment", i, post.ID, post.Comments)
		}
	}
}

// preloadCommentStatement is the statement a has-many preload sends for the
// children of a parent page, on each dialect. It is pinned from the live text
// each dialect's test database receives.
func preloadCommentStatement(dialect db.Dialect) string {
	switch dialect {
	case db.SQLite:
		return "SELECT `id`, `post_id`, `body` FROM `preload_comment` WHERE (`post_id` IN (?, ?)) ORDER BY `id` ASC"
	case db.Postgres:
		return `SELECT "id", "post_id", "body" FROM "preload_comment" WHERE ("post_id" IN ($1, $2)) ORDER BY "id" ASC`
	case db.MySQL:
		return "SELECT `id`, `post_id`, `body` FROM `preload_comment` WHERE (`post_id` IN (?, ?)) ORDER BY `id` ASC"
	}
	return ""
}

func TestListPreloadHasManyRelationStatementPinnedPerDialect(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	first := preloadPost{Title: "first"}
	second := preloadPost{Title: "second"}
	if err := store.Create(ctx, posts, &first); err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := store.Create(ctx, posts, &second); err != nil {
		t.Fatalf("create post: %v", err)
	}
	createPreloadComment(t, store, comments, first.ID, "c1")
	counter.Reset()

	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments"}}, &got); err != nil {
		t.Fatalf("List: %v", err)
	}
	statements := counter.Statements()
	if len(statements) != 2 {
		t.Fatalf("sent %d statements, want 2", len(statements))
	}
	if want := preloadCommentStatement(dialect); statements[1] != want {
		t.Errorf("child statement:\n got  %s\n want %s", statements[1], want)
	}
}

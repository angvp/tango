package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/querycount"
	"github.com/angvp/tango/model"
)

// pathNode is a self-referencing model with three relations, enough distinct
// dotted paths within MaxPreloadDepth to reach the path cap.
type pathNode struct {
	ID       int64      `tango:"pk"`
	ParentID int64      `tango:"fk=pathNode"`
	Parent   *pathNode  `tango:"belongs_to=ParentID"`
	MateID   int64      `tango:"fk=pathNode"`
	Mate     *pathNode  `tango:"belongs_to=MateID"`
	Children []pathNode `tango:"has_many=ParentID"`
}

func TestListPreloadNestedPathLoadsEveryLevel(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	beta := createPreloadAccount(t, store, accounts, "beta")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	p2 := createPreloadPost(t, store, posts, "p2", beta.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	createPreloadComment(t, store, comments, p2.ID, "c2")
	counter.Reset()

	var got []preloadPost
	query := db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments.Post.Account"}}
	if err := store.List(ctx, posts, query, &got); err != nil {
		t.Fatalf("List: %v", err)
	}
	// The rows, then one read per segment: comments, posts, accounts.
	if n := len(counter.Statements()); n != 4 {
		t.Fatalf("sent %d statements, want 4: rows, comments, posts, accounts", n)
	}
	if len(got) != 2 || len(got[0].Comments) != 1 || len(got[1].Comments) != 1 {
		t.Fatalf("got %+v, want one comment per post", got)
	}
	if name := got[0].Comments[0].Post.Account.Name; name != "alpha" {
		t.Errorf("first comment's post account = %q, want alpha", name)
	}
	if name := got[1].Comments[0].Post.Account.Name; name != "beta" {
		t.Errorf("second comment's post account = %q, want beta", name)
	}
}

func TestListPreloadNestedPathImpliesItsPrefixes(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")

	for _, names := range [][]string{{"Comments.Post"}, {"Comments", "Comments.Post"}} {
		counter.Reset()
		var got []preloadPost
		if err := store.List(ctx, posts, db.Query{Preload: names}, &got); err != nil {
			t.Fatalf("List %v: %v", names, err)
		}
		// Rows, comments once, posts once: the prefix Comments is read once.
		if n := counter.Matching("preload_comment"); n != 1 {
			t.Fatalf("Preload %v read comments %d times, want 1", names, n)
		}
		if len(got) != 1 || len(got[0].Comments) != 1 || got[0].Comments[0].Post == nil {
			t.Fatalf("Preload %v got %+v, want the comment with its post", names, got)
		}
	}
}

func TestListPreloadNestedPathDepthCap(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	// Known positive: three segments are accepted and do send statements.
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{Preload: []string{"Comments.Post.Account"}}, &got); err != nil {
		t.Fatalf("three segments refused: %v", err)
	}
	if n := len(counter.Statements()); n != 4 {
		t.Fatalf("three-segment path sent %d statements, want 4", n)
	}

	counter.Reset()
	err := store.List(ctx, posts, db.Query{Preload: []string{"Comments.Post.Comments.Post"}}, &got)
	var limit *db.QueryLimitError
	if !errors.As(err, &limit) || limit.Max != db.MaxPreloadDepth || limit.Got != 4 {
		t.Fatalf("four segments: err = %v, want QueryLimitError with Max %d and Got 4", err, db.MaxPreloadDepth)
	}
	if !strings.Contains(err.Error(), "MaxPreloadDepth") {
		t.Errorf("error %q does not name MaxPreloadDepth", err)
	}
	if n := len(counter.Statements()); n != 0 {
		t.Fatalf("refused path sent %d statements, want 0", n)
	}
}

func TestListPreloadNestedUnknownSegmentRefusedBeforeSQL(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	posts := metaFor(t, registry, preloadPost{})
	counter.Reset()

	var got []preloadPost
	err := store.List(ctx, posts, db.Query{Preload: []string{"Comments.Nope"}}, &got)
	var query *db.QueryError
	if !errors.As(err, &query) || !strings.Contains(err.Error(), "Comments.Nope") {
		t.Fatalf("err = %v, want a QueryError naming the full path Comments.Nope", err)
	}
	if n := len(counter.Statements()); n != 0 {
		t.Fatalf("refused path sent %d statements, want 0", n)
	}
}

// pathNodeNames returns count distinct dotted names over pathNode's relations
// Parent, Mate and Children. The first twelve names are every path of one or
// two segments, so each later name adds exactly one read.
func pathNodeNames(count int) []string {
	relations := []string{"Parent", "Mate", "Children"}
	var names []string
	for _, a := range relations {
		names = append(names, a)
	}
	for _, a := range relations {
		for _, b := range relations {
			names = append(names, a+"."+b)
		}
	}
	for _, a := range relations {
		for _, b := range relations {
			for _, c := range relations {
				names = append(names, a+"."+b+"."+c)
			}
		}
	}
	return names[:count]
}

func TestListPreloadNestedPathCountCap(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, pathNode{})
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	nodes := metaFor(t, registry, pathNode{})
	ctx := context.Background()

	var got []pathNode
	if err := store.List(ctx, nodes, db.Query{Preload: pathNodeNames(db.MaxPreloadPaths)}, &got); err != nil {
		t.Fatalf("%d distinct reads refused: %v", db.MaxPreloadPaths, err)
	}

	err := store.List(ctx, nodes, db.Query{Preload: pathNodeNames(db.MaxPreloadPaths + 1)}, &got)
	var limit *db.QueryLimitError
	if !errors.As(err, &limit) || limit.Max != db.MaxPreloadPaths || limit.Got != db.MaxPreloadPaths+1 {
		t.Fatalf("err = %v, want QueryLimitError with Max %d and Got %d", err, db.MaxPreloadPaths, db.MaxPreloadPaths+1)
	}
	if !strings.Contains(err.Error(), "MaxPreloadPaths") {
		t.Errorf("error %q does not name MaxPreloadPaths", err)
	}
}

func TestListPreloadNestedReadRunsInOneRepeatableReadTransaction(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{Preload: []string{"Comments.Post"}}, &got); err != nil {
		t.Fatalf("List: %v", err)
	}
	events := counter.Events()
	if kinds := transactionKinds(events); len(kinds) != 2 || kinds[0] != querycount.KindBegin || kinds[1] != querycount.KindCommit {
		t.Fatalf("recorded %v, want begin then commit", kinds)
	}
	if first := events[0]; first.Isolation != sql.LevelRepeatableRead || !first.ReadOnly {
		t.Fatalf("begin recorded isolation %v read-only %v, want repeatable read and read-only", first.Isolation, first.ReadOnly)
	}
	if n := len(counter.Statements()); n != 3 {
		t.Fatalf("sent %d statements inside the transaction, want 3: rows, comments, posts", n)
	}
}

func TestListPreloadNestedReadJoinsCallerTransaction(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	var got []preloadPost
	err := store.InTx(ctx, func(tx *db.Store) error {
		return tx.List(ctx, posts, db.Query{Preload: []string{"Comments.Post"}}, &got)
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if len(got) != 1 || len(got[0].Comments) != 1 {
		t.Fatalf("inside InTx got %+v, want the comment", got)
	}
	events := counter.Events()
	kinds := transactionKinds(events)
	if len(kinds) != 2 || kinds[0] != querycount.KindBegin || kinds[1] != querycount.KindCommit {
		t.Fatalf("recorded %v, want only InTx's begin and commit", kinds)
	}
	if first := events[0]; first.Isolation != sql.LevelDefault || first.ReadOnly {
		t.Fatalf("the one begin has isolation %v read-only %v, want InTx's default and false: the nested read opened its own transaction", first.Isolation, first.ReadOnly)
	}
}

func TestListPreloadNestedReadRollsBackOnLevelFailure(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	// The second read of preload_post is the post level of Comments.Post: the
	// first is the parent row read.
	counter.FailNthMatching("preload_post", 2, errPreloadRead)
	got := []preloadPost{{Title: "untouched"}}
	err := store.List(ctx, posts, db.Query{Preload: []string{"Comments.Post"}}, &got)
	if !errors.Is(err, errPreloadRead) {
		t.Fatalf("err = %v, want the injected level failure", err)
	}
	if kinds := transactionKinds(counter.Events()); len(kinds) != 2 || kinds[0] != querycount.KindBegin || kinds[1] != querycount.KindRollback {
		t.Fatalf("recorded %v, want begin then rollback", kinds)
	}
	if len(got) != 1 || got[0].Title != "untouched" {
		t.Fatalf("destination changed to %+v after a failed level", got)
	}
}

// renameAccount is the statement that renames the account with id to name.
func renameAccount(id int64, name string) string {
	return fmt.Sprintf("UPDATE preload_account SET name = '%s' WHERE id = %d", name, id)
}

// concurrentWrite runs a write on another connection of sqlDB, bounded by a
// context so a write the database refuses or cannot schedule returns an error
// instead of hanging the test.
func concurrentWrite(ctx context.Context, bound time.Duration, sqlDB *sql.DB, statement string) error {
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	_, err := sqlDB.ExecContext(ctx, statement)
	return err
}

// refusedWriteBound bounds a write that SQLite is expected to refuse while a
// nested read runs. On the in-memory single-connection pool the write waits
// for a connection and this bound ends the wait; on a file database the
// driver's busy timeout refuses it.
const refusedWriteBound = time.Second

// TestListPreloadNestedReadSeesOneSnapshotAcrossLevels asserts the guarantee
// each dialect gives a nested read (D25): no later level sees a write made
// during the read. PostgreSQL and MySQL give it by a read-only REPEATABLE READ
// snapshot, so the write commits and the later level does not contain it.
// SQLite gives it by blocking, so the write is refused while the read runs.
func TestListPreloadNestedReadSeesOneSnapshotAcrossLevels(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	if dialect == db.SQLite {
		testNestedReadRefusesWriteOnSQLite(t, ctx, sqlDB, store, counter, posts, alpha.ID)
		return
	}

	// Another connection renames alpha and commits just before the account
	// level of the nested read runs.
	var hookErr error
	counter.RunBeforeMatching("preload_account", 1, func() {
		hookErr = concurrentWrite(ctx, 5*time.Second, sqlDB, renameAccount(alpha.ID, "renamed"))
	})
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments.Post.Account"}}, &got); err != nil {
		t.Fatalf("nested List: %v", err)
	}
	if hookErr != nil {
		t.Fatalf("concurrent write between levels did not commit: %v", hookErr)
	}
	if len(got) != 1 || len(got[0].Comments) != 1 || got[0].Comments[0].Post == nil || got[0].Comments[0].Post.Account == nil {
		t.Fatalf("got %+v, want the comment, its post and its account", got)
	}
	if name := got[0].Comments[0].Post.Account.Name; name != "alpha" {
		t.Fatalf("account level saw %q, want the snapshot's alpha: the write committed between levels leaked into the read", name)
	}

	// Known positive: the write did commit, so a fresh read contains it.
	counter.Reset()
	counter.RunBeforeMatching("preload_account", 1, func() {
		hookErr = concurrentWrite(ctx, 5*time.Second, sqlDB, renameAccount(alpha.ID, "renamed-again"))
	})
	got = nil
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account"}}, &got); err != nil {
		t.Fatalf("one-level List: %v", err)
	}
	if hookErr != nil {
		t.Fatalf("positive control write did not commit: %v", hookErr)
	}
	if len(got) != 1 || got[0].Account == nil || got[0].Account.Name != "renamed-again" {
		t.Fatalf("one-level read got %+v, want the write it was committed alongside", got)
	}
}

// testNestedReadRefusesWriteOnSQLite asserts the SQLite form of the guarantee:
// a write attempted during a nested read is refused, the read completes with
// the snapshot it started from, and the same write succeeds once the read has
// ended, which shows the refusal came from the read and not from a broken
// writer.
func testNestedReadRefusesWriteOnSQLite(t *testing.T, ctx context.Context, sqlDB *sql.DB, store *db.Store, counter *querycount.Counter, posts model.ModelMeta, alphaID int64) {
	t.Helper()
	var hookErr error
	counter.RunBeforeMatching("preload_account", 1, func() {
		hookErr = concurrentWrite(ctx, refusedWriteBound, sqlDB, renameAccount(alphaID, "renamed"))
	})
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments.Post.Account"}}, &got); err != nil {
		t.Fatalf("nested List: %v", err)
	}
	if hookErr == nil {
		t.Fatalf("a write during the nested read committed; SQLite must refuse it while the read runs")
	}
	t.Logf("write refused during the nested read: %v", hookErr)
	if len(got) != 1 || got[0].Comments[0].Post.Account == nil {
		t.Fatalf("got %+v, want the comment, its post and its account", got)
	}
	if name := got[0].Comments[0].Post.Account.Name; name != "alpha" {
		t.Fatalf("account level saw %q, want alpha: the read is not consistent", name)
	}

	// Known positive: after the read ends the same write succeeds and a fresh
	// read contains it.
	if err := concurrentWrite(ctx, 5*time.Second, sqlDB, renameAccount(alphaID, "renamed")); err != nil {
		t.Fatalf("the same write after the read: %v (the refusal would then not be caused by the read)", err)
	}
	var fresh []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account"}}, &fresh); err != nil {
		t.Fatalf("fresh List: %v", err)
	}
	if len(fresh) != 1 || fresh[0].Account == nil || fresh[0].Account.Name != "renamed" {
		t.Fatalf("fresh read got %+v, want the committed rename", fresh)
	}
}

// nestedCommentStatement and nestedPostStatement are the statements the
// "Comments.Post" read sends for its comment and post levels, on each dialect.
// They are pinned from the live text each dialect's test database receives.
func nestedCommentStatement(dialect db.Dialect) string {
	switch dialect {
	case db.SQLite:
		return "SELECT `id`, `post_id`, `body` FROM `preload_comment` WHERE (`post_id` IN (?)) ORDER BY `id` ASC"
	case db.Postgres:
		return `SELECT "id", "post_id", "body" FROM "preload_comment" WHERE ("post_id" IN ($1)) ORDER BY "id" ASC`
	case db.MySQL:
		return "SELECT `id`, `post_id`, `body` FROM `preload_comment` WHERE (`post_id` IN (?)) ORDER BY `id` ASC"
	}
	return ""
}

func nestedPostStatement(dialect db.Dialect) string {
	switch dialect {
	case db.SQLite:
		return "SELECT `id`, `title`, `account_id` FROM `preload_post` WHERE (`id` IN (?))"
	case db.Postgres:
		return `SELECT "id", "title", "account_id" FROM "preload_post" WHERE ("id" IN ($1))`
	case db.MySQL:
		return "SELECT `id`, `title`, `account_id` FROM `preload_post` WHERE (`id` IN (?))"
	}
	return ""
}

func TestListPreloadNestedSQLPerDialect(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments.Post"}}, &got); err != nil {
		t.Fatalf("List: %v", err)
	}
	events := counter.Events()
	if len(events) != 5 || events[0].Kind != querycount.KindBegin || events[4].Kind != querycount.KindCommit {
		t.Fatalf("events = %+v, want begin, rows, comments, posts, commit", events)
	}
	statements := counter.Statements()
	if len(statements) != 3 {
		t.Fatalf("statements = %v, want 3", statements)
	}
	if want := nestedCommentStatement(dialect); statements[1] != want {
		t.Errorf("comment statement:\n got  %s\n want %s", statements[1], want)
	}
	if want := nestedPostStatement(dialect); statements[2] != want {
		t.Errorf("post statement:\n got  %s\n want %s", statements[2], want)
	}
}

func TestListPreloadNestedRevisitedModelLoadsEachLevel(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	comments := metaFor(t, registry, preloadComment{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	p1 := createPreloadPost(t, store, posts, "p1", alpha.ID)
	createPreloadComment(t, store, comments, p1.ID, "c1")
	counter.Reset()

	// preloadComment appears at the first level and again at the third, so the
	// path revisits the model and must still load each level for its own rows.
	var got []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Comments.Post.Comments"}}, &got); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || len(got[0].Comments) != 1 || len(got[0].Comments[0].Post.Comments) != 1 {
		t.Fatalf("got %+v, want the comment, its post, and the post's comments loaded", got)
	}
	if body := got[0].Comments[0].Post.Comments[0].Body; body != "c1" {
		t.Fatalf("revisited comment body = %q, want c1", body)
	}
}

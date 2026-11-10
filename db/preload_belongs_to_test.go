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

type preloadAccount struct {
	ID   int64  `tango:"pk"`
	Name string `tango:"varchar=100"`
}

type preloadPost struct {
	ID        int64            `tango:"pk"`
	Title     string           `tango:"varchar=200"`
	AccountID int64            `tango:"fk=preloadAccount,index"`
	Account   *preloadAccount  `tango:"belongs_to=AccountID"`
	Comments  []preloadComment `tango:"has_many=PostID"`
}

type preloadComment struct {
	ID     int64        `tango:"pk"`
	PostID int64        `tango:"fk=preloadPost,index"`
	Body   string       `tango:"varchar=200"`
	Post   *preloadPost `tango:"belongs_to=PostID"`
}

// errPreloadRead is the failure a fault-injected relation read returns.
var errPreloadRead = errors.New("injected relation read failure")

// preloadFixture opens a database holding the preload models, counts its
// statements, and returns a Store on it. The Store has no registry until a
// test calls UseModels.
func preloadFixture(t *testing.T) (*sql.DB, db.Dialect, *model.Registry, *querycount.Counter) {
	t.Helper()
	counter := querycount.Attach(t)
	sqlDB, dialect, registry := openTables(t, preloadAccount{}, preloadPost{}, preloadComment{})
	counter.Reset()
	return sqlDB, dialect, registry, counter
}

func createPreloadAccount(t *testing.T, store *db.Store, meta model.ModelMeta, name string) preloadAccount {
	t.Helper()
	account := preloadAccount{Name: name}
	if err := store.Create(context.Background(), meta, &account); err != nil {
		t.Fatalf("create account %s: %v", name, err)
	}
	return account
}

func createPreloadPost(t *testing.T, store *db.Store, meta model.ModelMeta, title string, accountID int64) preloadPost {
	t.Helper()
	post := preloadPost{Title: title, AccountID: accountID}
	if err := store.Create(context.Background(), meta, &post); err != nil {
		t.Fatalf("create post %s: %v", title, err)
	}
	return post
}

func TestListPreloadBelongsToLoadsEachRowsAccount(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	beta := createPreloadAccount(t, store, accounts, "beta")
	createPreloadPost(t, store, posts, "a1", alpha.ID)
	createPreloadPost(t, store, posts, "a2", alpha.ID)
	createPreloadPost(t, store, posts, "b1", beta.ID)
	createPreloadPost(t, store, posts, "unset", 0)
	counter.Reset()

	// Control: without Preload only the rows are read and no relation is set.
	var plain []preloadPost
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}}, &plain); err != nil {
		t.Fatalf("List without Preload: %v", err)
	}
	if len(counter.Statements()) != 1 || counter.Matching("preload_account") != 0 {
		t.Fatalf("control sent %v, want only the row read", counter.Statements())
	}
	if plain[0].Account != nil {
		t.Fatalf("Account = %+v without Preload, want nil", plain[0].Account)
	}

	counter.Reset()
	query := db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account"}}
	var got []preloadPost
	if err := store.List(ctx, posts, query, &got); err != nil {
		t.Fatalf("List with Preload: %v", err)
	}
	if n := len(counter.Statements()); n != 2 {
		t.Fatalf("sent %d statements, want 2: the rows and one account read for two distinct keys", n)
	}
	if n := counter.Matching("preload_account"); n != 1 {
		t.Fatalf("account read sent %d times, want 1", n)
	}
	if !reflect.DeepEqual(query, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account"}}) {
		t.Errorf("caller's query changed to %+v", query)
	}

	if len(got) != 4 {
		t.Fatalf("List returned %d rows, want 4", len(got))
	}
	if got[0].Account == nil || got[0].Account.Name != "alpha" || got[1].Account == nil || got[1].Account.Name != "alpha" {
		t.Errorf("rows sharing alpha loaded %+v and %+v", got[0].Account, got[1].Account)
	}
	if got[2].Account == nil || got[2].Account.Name != "beta" {
		t.Errorf("row for beta loaded %+v", got[2].Account)
	}
	if got[3].Account != nil {
		t.Errorf("unset key loaded %+v, want nil", got[3].Account)
	}
	if got[0].Comments != nil {
		t.Errorf("Comments = %v without being named, want nil", got[0].Comments)
	}

	got[0].Account.Name = "changed"
	if got[1].Account.Name != "alpha" {
		t.Errorf("rows share one related struct: second row now reads %q", got[1].Account.Name)
	}
}

func TestListPreloadBelongsToCountsOneReadPerThousandKeys(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})

	const count = db.MaxInValues + 1
	ids := make([]int64, 0, count)
	err := store.InTx(ctx, func(tx *db.Store) error {
		for i := 0; i < count; i++ {
			account := preloadAccount{Name: "n"}
			if err := tx.Create(ctx, accounts, &account); err != nil {
				return err
			}
			ids = append(ids, account.ID)
		}
		for _, id := range ids {
			post := preloadPost{Title: "p", AccountID: id}
			if err := tx.Create(ctx, posts, &post); err != nil {
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
	if err := store.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account"}}, &got); err != nil {
		t.Fatalf("List with Preload: %v", err)
	}
	if n := len(counter.Statements()); n != 3 {
		t.Fatalf("sent %d statements, want 3: the rows and two account reads for %d keys", n, count)
	}
	if n := counter.Matching("preload_account"); n != 2 {
		t.Fatalf("account reads = %d, want 2", n)
	}
	if len(got) != count || got[count-1].Account == nil || got[count-1].Account.ID != ids[count-1] {
		t.Fatalf("rows or relations not loaded across chunks")
	}
}

func TestListPreloadWithNoRowsSendsNoRelationStatement(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	createPreloadPost(t, store, posts, "a1", alpha.ID)
	counter.Reset()

	var none []preloadPost
	query := db.Query{Where: []db.Condition{{Field: "Title", Op: db.OpEq, Value: "missing"}}, Preload: []string{"Account"}}
	if err := store.List(ctx, posts, query, &none); err != nil {
		t.Fatalf("List: %v", err)
	}
	if n := len(counter.Statements()); n != 1 {
		t.Fatalf("no rows sent %d statements, want 1 (the row read only)", n)
	}

	// Control: the same request with a matching row sends the relation read.
	counter.Reset()
	query.Where = nil
	var some []preloadPost
	if err := store.List(ctx, posts, query, &some); err != nil {
		t.Fatalf("List control: %v", err)
	}
	if n := len(counter.Statements()); n != 2 {
		t.Fatalf("control sent %d statements, want 2", n)
	}
}

func TestListPreloadAppliesParentQueryToParentsOnly(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	beta := createPreloadAccount(t, store, accounts, "beta")
	createPreloadPost(t, store, posts, "a1", alpha.ID)
	createPreloadPost(t, store, posts, "b1", beta.ID)
	counter.Reset()

	var got []preloadPost
	query := db.Query{
		Where:   []db.Condition{{Field: "Title", Op: db.OpEq, Value: "a1"}},
		OrderBy: []string{"ID"},
		Limit:   1,
		Preload: []string{"Account"},
	}
	if err := store.List(ctx, posts, query, &got); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Title != "a1" || got[0].Account == nil || got[0].Account.Name != "alpha" {
		t.Fatalf("got %+v, want only a1 with its account", got)
	}
	if n := counter.Matching("preload_account"); n != 1 {
		t.Fatalf("account read sent %d times, want 1 for the one returned row", n)
	}
}

func TestFirstHonoursPreloadAndCountAndExistsIgnoreIt(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	createPreloadPost(t, store, posts, "a1", alpha.ID)
	counter.Reset()

	match := []db.Condition{{Field: "Title", Op: db.OpEq, Value: "a1"}}
	var plain preloadPost
	if err := store.First(ctx, posts, db.Query{Where: match}, &plain); err != nil {
		t.Fatalf("First without Preload: %v", err)
	}
	if plain.Account != nil {
		t.Fatalf("First without Preload set Account %+v", plain.Account)
	}

	var first preloadPost
	if err := store.First(ctx, posts, db.Query{Where: match, Preload: []string{"Account"}}, &first); err != nil {
		t.Fatalf("First with Preload: %v", err)
	}
	if first.Account == nil || first.Account.Name != "alpha" {
		t.Fatalf("First with Preload loaded %+v, want alpha", first.Account)
	}

	// Control: List with Preload reads the relation through the same instrument.
	counter.Reset()
	if _, err := store.Count(ctx, posts, db.Query{Preload: []string{"Account"}}); err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n := counter.Matching("preload_account"); n != 0 || len(counter.Statements()) != 1 {
		t.Fatalf("Count sent %v, want only its own statement", counter.Statements())
	}
	counter.Reset()
	if _, err := store.Exists(ctx, posts, db.Query{Preload: []string{"Account"}}); err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if n := counter.Matching("preload_account"); n != 0 || len(counter.Statements()) != 1 {
		t.Fatalf("Exists sent %v, want only its own statement", counter.Statements())
	}
	counter.Reset()
	var rows []preloadPost
	if err := store.List(ctx, posts, db.Query{Preload: []string{"Account"}}, &rows); err != nil {
		t.Fatalf("List control: %v", err)
	}
	if n := counter.Matching("preload_account"); n != 1 {
		t.Fatalf("control List read the relation %d times, want 1", n)
	}
}

func TestListPreloadRefusedBeforeAnySQL(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	posts := metaFor(t, registry, preloadPost{})
	ctx := context.Background()

	tests := []struct {
		name    string
		preload []string
	}{
		{"unknown name", []string{"Nope"}},
		{"dotted path, a later extension", []string{"Account.Name"}},
		{"repeated name", []string{"Account", "Account"}},
	}
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counter.Reset()
			var rows []preloadPost
			err := store.List(ctx, posts, db.Query{Preload: tc.preload}, &rows)
			var queryErr *db.QueryError
			if !errors.As(err, &queryErr) {
				t.Fatalf("List error = %v, want a *QueryError", err)
			}
			if n := len(counter.Statements()); n != 0 {
				t.Fatalf("refused request sent %d statements, want 0", n)
			}
		})
	}

	t.Run("store without UseModels", func(t *testing.T) {
		bare := db.NewStore(sqlDB, dialect)
		counter.Reset()
		var rows []preloadPost
		err := bare.List(ctx, posts, db.Query{Preload: []string{"Account"}}, &rows)
		var queryErr *db.QueryError
		if !errors.As(err, &queryErr) {
			t.Fatalf("List error = %v, want a *QueryError", err)
		}
		if n := len(counter.Statements()); n != 0 {
			t.Fatalf("refused request sent %d statements, want 0", n)
		}

		// Control: the same Store succeeds once it has its models.
		bare.UseModels(registry)
		if err := bare.List(ctx, posts, db.Query{Preload: []string{"Account"}}, &rows); err != nil {
			t.Fatalf("List after UseModels: %v", err)
		}
	})
}

func TestPreloadLeavesDestUntouchedWhenRelationReadFails(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})
	alpha := createPreloadAccount(t, store, accounts, "alpha")
	createPreloadPost(t, store, posts, "a1", alpha.ID)
	counter.Reset()

	// Control: the read succeeds before any failure is armed.
	var control []preloadPost
	if err := store.List(ctx, posts, db.Query{Preload: []string{"Account"}}, &control); err != nil {
		t.Fatalf("control List: %v", err)
	}
	if len(control) != 1 || control[0].Account == nil {
		t.Fatalf("control did not load the account: %+v", control)
	}

	counter.FailMatching("preload_account", errPreloadRead)
	sentinel := preloadPost{ID: 9, Title: "keep"}
	rows := []preloadPost{sentinel}
	err := store.List(ctx, posts, db.Query{Preload: []string{"Account"}}, &rows)
	if !errors.Is(err, errPreloadRead) {
		t.Fatalf("List error = %v, want the injected read failure", err)
	}
	if !reflect.DeepEqual(rows, []preloadPost{sentinel}) {
		t.Errorf("List changed dest on a relation failure: %+v", rows)
	}

	first := sentinel
	if err := store.First(ctx, posts, db.Query{Preload: []string{"Account"}}, &first); !errors.Is(err, errPreloadRead) {
		t.Fatalf("First error = %v, want the injected read failure", err)
	}
	if !reflect.DeepEqual(first, sentinel) {
		t.Errorf("First changed dest on a relation failure: %+v", first)
	}
}

func TestPreloadSQLGoldens(t *testing.T) {
	registry := registerModels(t, preloadAccount{}, preloadPost{}, preloadComment{})
	posts := metaFor(t, registry, preloadPost{})
	ctx := context.Background()

	for _, tc := range relatedDialects() {
		t.Run(tc.name, func(t *testing.T) {
			// With no rows the relation read is never sent, so the parent
			// statement must be byte-identical with and without Preload.
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
			if err := preloadStore.List(ctx, posts, db.Query{OrderBy: []string{"ID"}, Preload: []string{"Account"}}, &rows); err != nil {
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

func TestListPreloadInsideTransactionUsesCallerTransaction(t *testing.T) {
	sqlDB, dialect, registry, counter := preloadFixture(t)
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	ctx := context.Background()
	accounts := metaFor(t, registry, preloadAccount{})
	posts := metaFor(t, registry, preloadPost{})

	err := store.InTx(ctx, func(tx *db.Store) error {
		account := createPreloadAccount(t, tx, accounts, "uncommitted")
		createPreloadPost(t, tx, posts, "u1", account.ID)
		counter.Reset()
		var got []preloadPost
		if err := tx.List(ctx, posts, db.Query{Preload: []string{"Account"}}, &got); err != nil {
			return err
		}
		if len(got) != 1 || got[0].Account == nil || got[0].Account.Name != "uncommitted" {
			t.Errorf("inside InTx got %+v, want the uncommitted account", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
}

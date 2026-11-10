package db_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/db"
)

type relFieldAccount struct {
	ID   int64  `tango:"pk"`
	Name string `tango:"varchar=100"`
}

type relFieldPost struct {
	ID        int64            `tango:"pk"`
	AccountID int64            `tango:"fk=relFieldAccount,index"`
	Title     string           `tango:"varchar=200"`
	Account   *relFieldAccount `tango:"belongs_to=AccountID"`
}

// Relation fields are not columns: Create, Update, Get and List read and write
// only the columns, so a relation set on a row is neither stored nor read back.
func TestRelationFieldsAreIgnoredByCreateUpdateGetAndList(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, relFieldAccount{}, relFieldPost{})
	postMeta, _ := registry.Get("relFieldPost")
	store := db.NewStore(sqlDB, dialect)
	accountMeta, _ := registry.Get("relFieldAccount")
	for _, id := range []int64{7, 8} {
		account := relFieldAccount{ID: id, Name: "account"}
		if err := store.Create(ctx, accountMeta, &account); err != nil {
			t.Fatalf("Create account %d: %v", id, err)
		}
	}

	// The set relation is the stale value a write must ignore: the column
	// written is AccountID, and the Account pointer is never sent.
	created := relFieldPost{AccountID: 7, Title: "first", Account: &relFieldAccount{ID: 99, Name: "ignored"}}
	if err := store.Create(ctx, postMeta, &created); err != nil {
		t.Fatalf("Create with a relation set: %v", err)
	}

	var got relFieldPost
	if err := store.Get(ctx, postMeta, created.ID, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccountID != 7 || got.Title != "first" {
		t.Fatalf("Get columns = AccountID %d, Title %q; want 7, %q", got.AccountID, got.Title, "first")
	}
	if got.Account != nil {
		t.Fatalf("Get populated the relation field %+v; reads must not fill it", got.Account)
	}

	// Update writes the columns, not the relation: the Account set here must
	// not be written, and AccountID must be.
	got.AccountID = 8
	got.Title = "second"
	got.Account = &relFieldAccount{ID: 100}
	if err := store.Update(ctx, postMeta, &got); err != nil {
		t.Fatalf("Update with a relation set: %v", err)
	}

	var reread relFieldPost
	if err := store.Get(ctx, postMeta, created.ID, &reread); err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if reread.AccountID != 8 || reread.Title != "second" {
		t.Fatalf("Update wrote AccountID %d, Title %q; want 8, %q", reread.AccountID, reread.Title, "second")
	}

	var rows []relFieldPost
	if err := store.List(ctx, postMeta, db.Query{}, &rows); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List returned %d rows, want 1", len(rows))
	}
	if rows[0].AccountID != 8 || rows[0].Title != "second" || rows[0].Account != nil {
		t.Fatalf("List row = %+v, want columns from the update and no loaded relation", rows[0])
	}
}

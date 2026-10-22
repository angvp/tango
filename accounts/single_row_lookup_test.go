package accounts_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// The accounts lookups read one row with Store.First (primary-key order by
// default). That is equivalent to an unordered read only because each
// predicate names a unique column; these tests pin the constraints.
func TestAccountSingleRowLookupsRestOnUniqueColumns(t *testing.T) {
	registry := model.NewRegistry()
	for _, m := range []any{accounts.Account{}, accounts.AccountToken{}} {
		if err := registry.Register(m); err != nil {
			t.Fatalf("register %T: %v", m, err)
		}
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, registry.All())
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()
	accountMeta, _ := registry.Get("Account")
	tokenMeta, _ := registry.Get("AccountToken")

	account := accounts.Account{Email: "a@example.com", Active: true}
	if err := store.Create(ctx, accountMeta, &account); err != nil {
		t.Fatal(err)
	}
	dup := accounts.Account{Email: "a@example.com"}
	if err := store.Create(ctx, accountMeta, &dup); !db.IsUniqueConstraintViolation(err) {
		t.Errorf("duplicate Account.Email: err = %v, want a unique violation", err)
	}

	token := accounts.AccountToken{AccountID: account.ID, TokenHash: "hash"}
	if err := store.Create(ctx, tokenMeta, &token); err != nil {
		t.Fatal(err)
	}
	again := accounts.AccountToken{AccountID: account.ID, TokenHash: "hash"}
	if err := store.Create(ctx, tokenMeta, &again); !db.IsUniqueConstraintViolation(err) {
		t.Errorf("duplicate AccountToken.TokenHash: err = %v, want a unique violation", err)
	}
}

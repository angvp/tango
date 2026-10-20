package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

type mysqlAccount struct {
	ID        int64  `tango:"pk"`
	Email     string `tango:"unique"`
	Handle    string `tango:"index"`
	Note      string
	Nickname  string `tango:"varchar=10"`
	CreatedAt time.Time
}

func openMySQLAccounts(t *testing.T) (*db.Store, model.ModelMeta) {
	t.Helper()
	sqlDB, dialect, registry := openTables(t, mysqlAccount{})
	return db.NewStore(sqlDB, dialect), metaFor(t, registry, mysqlAccount{})
}

func TestUniqueOrIndexedBareStringIsBoundedAt255RunesOnMySQLOnly(t *testing.T) {
	store, meta := openMySQLAccounts(t)
	ctx := context.Background()
	long := strings.Repeat("é", 256)
	exact := strings.Repeat("é", 255)

	err := store.Create(ctx, meta, &mysqlAccount{Email: long, Handle: "h"})
	var tooLong *db.ValueTooLongError
	if testdb.Dialect() == db.MySQL {
		if !errors.As(err, &tooLong) || tooLong.Field != "Email" || tooLong.Max != 255 || tooLong.Got != 256 {
			t.Fatalf("Create with a 256-rune unique string = %v, want *ValueTooLongError{Email, Max 255, Got 256}", err)
		}
		if err := store.Create(ctx, meta, &mysqlAccount{Email: exact, Handle: "h"}); err != nil {
			t.Fatalf("Create with exactly 255 runes: %v", err)
		}
		if err := store.Create(ctx, meta, &mysqlAccount{Email: "a", Handle: long}); !errors.As(err, &tooLong) || tooLong.Field != "Handle" {
			t.Fatalf("Create with a 256-rune indexed string = %v, want a ValueTooLongError for Handle", err)
		}
		account := mysqlAccount{Email: "b", Handle: "h"}
		if err := store.Create(ctx, meta, &account); err != nil {
			t.Fatal(err)
		}
		account.Email = long
		if err := store.Update(ctx, meta, &account); !errors.As(err, &tooLong) {
			t.Fatalf("Update with a 256-rune unique string = %v, want a ValueTooLongError", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Create with a long unique string on a dialect that indexes text: %v", err)
	}
}

func TestOnlyUnboundedUniqueOrIndexedStringsGetTheMySQLBound(t *testing.T) {
	store, meta := openMySQLAccounts(t)
	ctx := context.Background()
	long := strings.Repeat("x", 10000)

	if err := store.Create(ctx, meta, &mysqlAccount{Email: "n", Handle: "n", Note: long}); err != nil {
		t.Fatalf("a plain string stays unbounded: %v", err)
	}
	var tooLong *db.ValueTooLongError
	err := store.Create(ctx, meta, &mysqlAccount{Email: "m", Handle: "m", Nickname: strings.Repeat("y", 11)})
	if !errors.As(err, &tooLong) || tooLong.Max != 10 {
		t.Fatalf("an explicit varchar keeps its own bound: %v", err)
	}
}

func TestTimesRoundTripInUTCAndAnUnsetTimeStaysUnset(t *testing.T) {
	store, meta := openMySQLAccounts(t)
	ctx := context.Background()
	zone := time.FixedZone("UTC+9", 9*3600)
	if testdb.Dialect() == db.SQLite {
		zone = time.UTC // the SQLite driver reads back only the times it wrote in UTC
	}
	moment := time.Date(2026, 10, 19, 12, 30, 45, 123456000, zone)

	set := mysqlAccount{Email: "set", Handle: "s", CreatedAt: moment}
	unset := mysqlAccount{Email: "unset", Handle: "u"}
	for _, a := range []*mysqlAccount{&set, &unset} {
		if err := store.Create(ctx, meta, a); err != nil {
			t.Fatal(err)
		}
	}
	var gotSet, gotUnset mysqlAccount
	if err := store.Get(ctx, meta, set.ID, &gotSet); err != nil {
		t.Fatal(err)
	}
	if err := store.Get(ctx, meta, unset.ID, &gotUnset); err != nil {
		t.Fatal(err)
	}
	if !gotSet.CreatedAt.Equal(moment) {
		t.Errorf("CreatedAt = %v, want the same instant as %v", gotSet.CreatedAt, moment)
	}
	if !gotUnset.CreatedAt.IsZero() {
		t.Errorf("an unset time read back as %v, want the zero time", gotUnset.CreatedAt)
	}
}

func TestMySQLConnectionsReadCommittedAndCountMatchedRows(t *testing.T) {
	testdb.MySQLOnly(t, "reads MySQL session settings")
	sqlDB, _ := testdb.Open(t)
	var isolation, mode string
	if err := sqlDB.QueryRow("SELECT @@transaction_isolation, @@sql_mode").Scan(&isolation, &mode); err != nil {
		t.Fatal(err)
	}
	if isolation != "READ-COMMITTED" || !strings.Contains(mode, "STRICT_TRANS_TABLES") {
		t.Fatalf("isolation %q, sql_mode %q; want READ-COMMITTED and strict mode", isolation, mode)
	}
}

func TestUpdateOfAnUnchangedRowIsNotReportedAsMissing(t *testing.T) {
	store, meta := openMySQLAccounts(t)
	ctx := context.Background()
	account := mysqlAccount{Email: "same", Handle: "h"}
	if err := store.Create(ctx, meta, &account); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, meta, &account); err != nil {
		t.Fatalf("Update writing the values already stored: %v (MySQL must count matched rows)", err)
	}
}

func TestLikeIsCaseSensitiveOnMySQLAndPostgreSQL(t *testing.T) {
	store, meta := openMySQLAccounts(t)
	ctx := context.Background()
	if err := store.Create(ctx, meta, &mysqlAccount{Email: "Alice", Handle: "h"}); err != nil {
		t.Fatal(err)
	}
	var found []mysqlAccount
	if err := store.List(ctx, meta, db.Query{Where: []db.Condition{{Field: "Email", Op: db.OpLike, Value: "al%"}}}, &found); err != nil {
		t.Fatal(err)
	}
	wantMatch := testdb.Dialect() == db.SQLite // SQLite folds ASCII case in LIKE; the others do not
	if (len(found) == 1) != wantMatch {
		t.Fatalf("LIKE 'al%%' matched %d rows of 'Alice'; case-insensitive match expected = %v", len(found), wantMatch)
	}
}

type mysqlSlug struct {
	Slug string `tango:"pk"`
}

func TestStringPrimaryKeyIsBoundedAt255RunesOnMySQL(t *testing.T) {
	testdb.MySQLOnly(t, "the implicit bound exists only where unbounded text cannot be indexed")
	sqlDB, dialect, registry := openTables(t, mysqlSlug{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, mysqlSlug{})
	err := store.Create(context.Background(), meta, &mysqlSlug{Slug: strings.Repeat("é", 256)})
	var tooLong *db.ValueTooLongError
	if !errors.As(err, &tooLong) || tooLong.Field != "Slug" || tooLong.Max != 255 {
		t.Fatalf("Create with a 256-rune primary key = %v, want *ValueTooLongError{Slug, Max 255}", err)
	}
}

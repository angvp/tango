package admin

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

func adminLookupStore(t *testing.T) *db.Store {
	t.Helper()
	models := model.NewRegistry()
	for _, m := range []any{AdminUser{}, AdminSession{}} {
		if err := models.Register(m); err != nil {
			t.Fatalf("register %T: %v", m, err)
		}
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	return db.NewStore(sqlDB, dialect)
}

// The lookups below read one row with Store.First, which orders by primary
// key when nothing else is asked. That is only equivalent to an unordered
// single-row read because each predicate names a unique column: the tests
// pin the database constraint that makes it so, and the not-found paths.
func TestAdminSingleRowLookupsRestOnUniqueColumns(t *testing.T) {
	store := adminLookupStore(t)
	ctx := context.Background()
	userMeta, sessionMeta := adminModelMetas()

	user := AdminUser{Username: "ada", Active: true}
	if err := store.Create(ctx, userMeta, &user); err != nil {
		t.Fatal(err)
	}
	duplicate := AdminUser{Username: "ada"}
	if err := store.Create(ctx, userMeta, &duplicate); !db.IsUniqueConstraintViolation(err) {
		t.Errorf("a second AdminUser with the same Username: err = %v, want a unique violation", err)
	}

	session := AdminSession{Token: "tok", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Create(ctx, sessionMeta, &session); err != nil {
		t.Fatal(err)
	}
	again := AdminSession{Token: "tok", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Create(ctx, sessionMeta, &again); !db.IsUniqueConstraintViolation(err) {
		t.Errorf("a second AdminSession with the same Token: err = %v, want a unique violation", err)
	}
}

func TestAdminSingleRowLookupsReportNoRowWithoutError(t *testing.T) {
	store := adminLookupStore(t)
	ctx := context.Background()

	if _, ok, err := findAdminUserByUsername(ctx, store, "nobody"); ok || err != nil {
		t.Errorf("findAdminUserByUsername = ok %v, err %v, want no row and no error", ok, err)
	}
	if _, ok, err := sessionUser(ctx, store, "unknown-token"); ok || err != nil {
		t.Errorf("sessionUser with an unknown token = ok %v, err %v", ok, err)
	}
	if err := deleteSessionByToken(ctx, store, "unknown-token"); err != nil {
		t.Errorf("deleteSessionByToken of an absent token = %v, want nil", err)
	}

	userMeta, sessionMeta := adminModelMetas()
	user := AdminUser{Username: "ada", Active: true}
	if err := store.Create(ctx, userMeta, &user); err != nil {
		t.Fatal(err)
	}
	// A session whose user row is gone is not a valid session.
	orphan := AdminSession{Token: "orphan", UserID: user.ID + 100, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Create(ctx, sessionMeta, &orphan); err != nil {
		// SQLite/others may enforce the foreign key; then there is nothing to read.
		t.Skipf("orphan session refused by the database: %v", err)
	}
	if _, ok, err := sessionUser(ctx, store, "orphan"); ok || err != nil {
		t.Errorf("sessionUser with a missing user = ok %v, err %v", ok, err)
	}
}

func TestDeleteSessionByTokenRemovesOnlyThatSession(t *testing.T) {
	store := adminLookupStore(t)
	ctx := context.Background()
	userMeta, sessionMeta := adminModelMetas()
	user := AdminUser{Username: "ada", Active: true}
	if err := store.Create(ctx, userMeta, &user); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"one", "two"} {
		session := AdminSession{Token: token, UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
		if err := store.Create(ctx, sessionMeta, &session); err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteSessionByToken(ctx, store, "one"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := sessionUser(ctx, store, "one"); ok {
		t.Error("session one still valid after deletion")
	}
	if _, ok, _ := sessionUser(ctx, store, "two"); !ok {
		t.Error("session two was removed along with one")
	}
}

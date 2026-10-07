package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
)

// User and Order are named after SQL reserved words on purpose: their
// tables are "user" and "order", and their columns include "order",
// "group", "select" and "user". Generated SQL must quote every identifier
// for these to work on PostgreSQL (and, for "order", "group" and "select",
// on SQLite too).
type User struct {
	ID     int64 `tango:"pk"`
	Name   string
	Order  int
	Group  string `tango:"unique"`
	Select string `tango:"index"`
}

type Order struct {
	ID    int64 `tango:"pk"`
	User  int64 `tango:"fk=User"`
	Group string
}

func TestStoreRoundTripsReservedWordTableAndColumnNames(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, User{}, Order{})
	userMeta, _ := registry.Get("User")
	orderMeta, _ := registry.Get("Order")
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	ada := User{Name: "Ada", Order: 2, Group: "admins", Select: "a"}
	bob := User{Name: "Bob", Order: 1, Group: "staff", Select: "b"}
	for _, u := range []*User{&ada, &bob} {
		if err := store.Create(ctx, userMeta, u); err != nil {
			t.Fatalf("Create user %s: %v", u.Name, err)
		}
	}
	order := Order{User: ada.ID, Group: "first"}
	if err := store.Create(ctx, orderMeta, &order); err != nil {
		t.Fatalf("Create order: %v", err)
	}
	if err := store.Create(ctx, orderMeta, &Order{User: 9999}); !errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("Create order with dangling user: error = %v, want ErrInvalidForeignKey", err)
	}

	var got User
	if err := store.Get(ctx, userMeta, ada.ID, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != ada {
		t.Fatalf("Get = %+v, want %+v", got, ada)
	}

	ada.Order = 3
	if err := store.Update(ctx, userMeta, &ada); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var ordered []User
	query := db.Query{
		Where:   []db.Condition{{Field: "Order", Op: db.OpGte, Value: 1}},
		Any:     []db.Condition{{Field: "Group", Op: db.OpLike, Value: "adm%"}, {Field: "Select", Op: db.OpEq, Value: "b"}},
		OrderBy: []string{"-Order"},
	}
	if err := store.List(ctx, userMeta, query, &ordered); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ordered) != 2 || ordered[0].Name != "Ada" || ordered[0].Order != 3 || ordered[1].Name != "Bob" {
		t.Fatalf("List = %+v, want Ada (Order 3) then Bob", ordered)
	}

	count, err := store.Count(ctx, userMeta, db.Query{Any: []db.Condition{{Field: "Group", Op: db.OpLike, Value: "adm%"}}})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Count = %d, want 1", count)
	}

	if err := store.Delete(ctx, userMeta, ada.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Get(ctx, orderMeta, order.ID, &Order{}); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Get cascaded order: error = %v, want ErrNotFound", err)
	}
	remaining, err := store.Count(ctx, userMeta, db.Query{})
	if err != nil {
		t.Fatalf("Count remaining: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining users = %d, want 1", remaining)
	}
}

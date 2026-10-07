package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
)

func TestStoreListWhere(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	firstTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, widget := range []createWidget{
		{Name: "", Active: false, Count: 0, Score: 0, CreatedAt: firstTime},
		{Name: "later", Active: true, Count: 5, Score: 1.5, CreatedAt: firstTime.Add(time.Hour)},
		{Name: "last", Active: true, Count: 10, Score: 3, CreatedAt: firstTime.Add(2 * time.Hour)},
	} {
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		where []db.Condition
		want  int
	}{
		{"nil", nil, 3},
		{"empty", []db.Condition{}, 3},
		{"eq false", []db.Condition{{"Active", db.OpEq, false}}, 1},
		{"eq zero", []db.Condition{{"Count", db.OpEq, 0}}, 1},
		{"eq empty", []db.Condition{{"Name", db.OpEq, ""}}, 1},
		{"ne", []db.Condition{{"Name", db.OpNe, "later"}}, 2},
		{"gt", []db.Condition{{"Count", db.OpGt, 5}}, 1},
		{"gte", []db.Condition{{"Count", db.OpGte, 5}}, 2},
		{"lt", []db.Condition{{"Score", db.OpLt, float64(1.5)}}, 1},
		{"lte", []db.Condition{{"Score", db.OpLte, float64(1.5)}}, 2},
		{"time range", []db.Condition{{"CreatedAt", db.OpGte, firstTime}, {"CreatedAt", db.OpLte, firstTime.Add(time.Hour)}}, 2},
		{"and fields", []db.Condition{{"Active", db.OpEq, true}, {"Count", db.OpGt, 5}}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []createWidget
			if err := store.List(context.Background(), meta, db.Query{Where: test.where}, &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != test.want {
				t.Fatalf("got %d rows, want %d", len(got), test.want)
			}
		})
	}
}

func TestStoreListWhereValidation(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := db.NewStore(nil, db.SQLite)
	tests := []struct {
		name      string
		condition db.Condition
		want      string
	}{
		{"unknown field", db.Condition{"Missing", db.OpEq, 1}, "unknown Where field"},
		{"unknown operator", db.Condition{"Count", db.Op("oops"), 1}, "unsupported Where operator"},
		{"string comparison", db.Condition{"Name", db.OpGt, "a"}, "not supported"},
		{"bool comparison", db.Condition{"Active", db.OpLt, true}, "not supported"},
		{"nil value", db.Condition{"Name", db.OpEq, nil}, "nil Where value"},
		{"wrong kind", db.Condition{"Count", db.OpEq, "1"}, "has type"},
		{"wrong width", db.Condition{"ID", db.OpEq, int(1)}, "has type"},
		{"wrong time type", db.Condition{"CreatedAt", db.OpEq, struct{}{}}, "has type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []createWidget
			err := store.List(context.Background(), meta, db.Query{Where: []db.Condition{test.condition}}, &got)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStoreListAnyLikeAndCount(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	for _, widget := range []createWidget{
		{Name: "Alpha", Active: true},
		{Name: "Beta", Active: true},
		{Name: "Alpine", Active: false},
	} {
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatal(err)
		}
	}
	query := db.Query{
		Where: []db.Condition{{Field: "Active", Op: db.OpEq, Value: true}},
		Any: []db.Condition{
			{Field: "Name", Op: db.OpLike, Value: "Al%"},
			{Field: "Name", Op: db.OpLike, Value: "%eta"},
		},
		OrderBy: []string{"Name"},
		Limit:   1,
		Offset:  1,
	}
	var got []createWidget
	if err := store.List(context.Background(), meta, query, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Beta" {
		t.Fatalf("List = %+v, want Beta", got)
	}
	count, err := store.Count(context.Background(), meta, query)
	if err != nil || count != 2 {
		t.Fatalf("Count = %d, %v; want 2", count, err)
	}
	for _, test := range []struct {
		name  string
		query db.Query
		want  int
	}{
		{"no filter", db.Query{}, 3},
		{"empty Any", db.Query{Any: []db.Condition{}}, 3},
		{"only Any", db.Query{Any: []db.Condition{{Field: "Name", Op: db.OpLike, Value: "Al%"}}}, 2},
		{"only Where", db.Query{Where: []db.Condition{{Field: "Active", Op: db.OpEq, Value: false}}}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			count, err := store.Count(context.Background(), meta, test.query)
			if err != nil || count != test.want {
				t.Fatalf("Count = %d, %v; want %d", count, err, test.want)
			}
		})
	}
}

func TestStoreAnyLikeValidation(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := db.NewStore(nil, db.SQLite)
	for _, test := range []struct {
		name      string
		condition db.Condition
		want      string
	}{
		{"unknown field", db.Condition{"Missing", db.OpLike, "x%"}, "unknown Where field"},
		{"nonstring field", db.Condition{"Count", db.OpLike, "1%"}, "only supported for string"},
		{"wrong value", db.Condition{"Name", db.OpLike, 1}, "has type"},
		{"nil value", db.Condition{"Name", db.OpLike, nil}, "nil Where value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := db.Query{Any: []db.Condition{test.condition}}
			_, err := store.Count(context.Background(), meta, query)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Count error = %v, want %q", err, test.want)
			}
			var got []createWidget
			err = store.List(context.Background(), meta, query, &got)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("List error = %v, want %q", err, test.want)
			}
		})
	}
}

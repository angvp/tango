package db

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStoreListWhereSQLite(t *testing.T) {
	sqlDB := openCreateTestDB(t)
	createWidgetTable(t, sqlDB)
	meta := registerModel(t, createWidget{})
	store := NewStore(sqlDB, SQLite)
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
		where []Condition
		want  int
	}{
		{"nil", nil, 3},
		{"empty", []Condition{}, 3},
		{"eq false", []Condition{{"Active", OpEq, false}}, 1},
		{"eq zero", []Condition{{"Count", OpEq, 0}}, 1},
		{"eq empty", []Condition{{"Name", OpEq, ""}}, 1},
		{"ne", []Condition{{"Name", OpNe, "later"}}, 2},
		{"gt", []Condition{{"Count", OpGt, 5}}, 1},
		{"gte", []Condition{{"Count", OpGte, 5}}, 2},
		{"lt", []Condition{{"Score", OpLt, float64(1.5)}}, 1},
		{"lte", []Condition{{"Score", OpLte, float64(1.5)}}, 2},
		{"time range", []Condition{{"CreatedAt", OpGte, firstTime}, {"CreatedAt", OpLte, firstTime.Add(time.Hour)}}, 2},
		{"and fields", []Condition{{"Active", OpEq, true}, {"Count", OpGt, 5}}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []createWidget
			if err := store.List(context.Background(), meta, Query{Where: test.where}, &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != test.want {
				t.Fatalf("got %d rows, want %d", len(got), test.want)
			}
		})
	}
}

func TestStoreWhereClausePostgresPlaceholderOrder(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := NewStore(nil, Postgres)
	clause, args, err := store.whereClause(meta, Query{
		Where: []Condition{{Field: "Active", Op: OpEq, Value: true}},
		Any: []Condition{
			{Field: "Name", Op: OpLike, Value: "Al%"},
			{Field: "Name", Op: OpLike, Value: "Be%"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantClause := " WHERE (active = $1) AND (name LIKE $2 OR name LIKE $3)"
	if clause != wantClause || !reflect.DeepEqual(args, []any{true, "Al%", "Be%"}) {
		t.Fatalf("clause = %q, args = %#v; want %q and ordered args", clause, args, wantClause)
	}
}

func TestStoreListWhereValidation(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := NewStore(nil, SQLite)
	tests := []struct {
		name      string
		condition Condition
		want      string
	}{
		{"unknown field", Condition{"Missing", OpEq, 1}, "unknown Where field"},
		{"unknown operator", Condition{"Count", Op("oops"), 1}, "unsupported Where operator"},
		{"string comparison", Condition{"Name", OpGt, "a"}, "not supported"},
		{"bool comparison", Condition{"Active", OpLt, true}, "not supported"},
		{"nil value", Condition{"Name", OpEq, nil}, "nil Where value"},
		{"wrong kind", Condition{"Count", OpEq, "1"}, "has type"},
		{"wrong width", Condition{"ID", OpEq, int(1)}, "has type"},
		{"wrong time type", Condition{"CreatedAt", OpEq, struct{}{}}, "has type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []createWidget
			err := store.List(context.Background(), meta, Query{Where: []Condition{test.condition}}, &got)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStoreListAnyLikeAndCountSQLite(t *testing.T) {
	sqlDB := openCreateTestDB(t)
	createWidgetTable(t, sqlDB)
	meta := registerModel(t, createWidget{})
	store := NewStore(sqlDB, SQLite)
	for _, widget := range []createWidget{
		{Name: "Alpha", Active: true},
		{Name: "Beta", Active: true},
		{Name: "Alpine", Active: false},
	} {
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatal(err)
		}
	}
	query := Query{
		Where: []Condition{{Field: "Active", Op: OpEq, Value: true}},
		Any: []Condition{
			{Field: "Name", Op: OpLike, Value: "Al%"},
			{Field: "Name", Op: OpLike, Value: "%eta"},
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
		query Query
		want  int
	}{
		{"no filter", Query{}, 3},
		{"empty Any", Query{Any: []Condition{}}, 3},
		{"only Any", Query{Any: []Condition{{Field: "Name", Op: OpLike, Value: "Al%"}}}, 2},
		{"only Where", Query{Where: []Condition{{Field: "Active", Op: OpEq, Value: false}}}, 1},
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
	store := NewStore(nil, SQLite)
	for _, test := range []struct {
		name      string
		condition Condition
		want      string
	}{
		{"unknown field", Condition{"Missing", OpLike, "x%"}, "unknown Where field"},
		{"nonstring field", Condition{"Count", OpLike, "1%"}, "only supported for string"},
		{"wrong value", Condition{"Name", OpLike, 1}, "has type"},
		{"nil value", Condition{"Name", OpLike, nil}, "nil Where value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := Query{Any: []Condition{test.condition}}
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
